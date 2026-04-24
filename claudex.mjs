#!/usr/bin/env node

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { createInterface } from 'node:readline';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const CONFIG_FILENAME = 'claudex.config.json';
const EXAMPLE_CONFIG = 'claudex.config.example.json';
const CLAUDE_CMD = 'claude';

// ── Config Loading ──────────────────────────────────────────────

function loadConfig() {
  const configPath = process.env.CLAUDEX_CONFIG || join(__dirname, CONFIG_FILENAME);

  if (!existsSync(configPath)) {
    return { profiles: [], _configPath: configPath, _missing: true };
  }

  try {
    const raw = readFileSync(configPath, 'utf-8');
    const config = JSON.parse(raw);

    if (!config.profiles || !Array.isArray(config.profiles)) {
      console.error(`Error: "profiles" 배열이 ${configPath}에 없습니다.`);
      process.exit(1);
    }

    for (const p of config.profiles) {
      if (!p.name) {
        console.error('Error: 프로파일에 "name" 필드가 없습니다.');
        process.exit(1);
      }
    }

    config._configPath = configPath;
    return config;
  } catch (e) {
    console.error(`설정 파일 읽기 오류: ${e.message}`);
    process.exit(1);
  }
}

// ── Argument Parsing ────────────────────────────────────────────

function parseArgs() {
  const args = process.argv.slice(2);
  let profileName = null;
  const claudeArgs = [];

  let i = 0;
  while (i < args.length) {
    if (args[i] === '-xSet') {
      if (i + 1 >= args.length) {
        console.error('Error: -xSet 뒤에 프로파일 이름이 필요합니다.');
        console.error('사용법: claudex -xSet "프로파일 이름" [claude 옵션...]');
        process.exit(1);
      }
      profileName = args[i + 1];
      i += 2;
    } else {
      claudeArgs.push(args[i]);
      i++;
    }
  }

  return { profileName, claudeArgs };
}

// ── Interactive Menu ────────────────────────────────────────────

// 반환값(action 객체):
//   { kind: 'launch', profile }            — 프로파일 실행
//   { kind: 'add' }                        — 새 프로바이더 추가 플로우
//   { kind: 'edit',   index, profile }     — 편집 플로우
//   { kind: 'delete', index, profile }     — 삭제 플로우
async function selectProfile(profiles) {
  const items = [
    ...profiles.map((p, i) => ({ kind: 'profile', profile: p, index: i })),
    { kind: 'add' },
  ];

  return new Promise((resolve) => {
    let selected = 0;
    const stdin = process.stdin;

    if (!stdin.isTTY) {
      console.error('Error: 인터랙티브 모드에는 TTY가 필요합니다.');
      console.error('-xSet 옵션으로 프로파일을 지정하세요.');
      process.exit(1);
    }

    stdin.setRawMode(true);
    stdin.resume();
    stdin.setEncoding('utf-8');

    function render() {
      process.stdout.write('\x1B[2J\x1B[H');
      console.log('');
      console.log('  \x1B[1m\x1B[36m claudex \x1B[0m\x1B[90m— 프로파일을 선택하세요\x1B[0m');
      console.log('');

      if (profiles.length === 0) {
        console.log('   \x1B[90m(등록된 프로파일이 없습니다)\x1B[0m');
        console.log('');
      }

      items.forEach((item, i) => {
        const cursor = i === selected ? '\x1B[33m❯\x1B[0m' : ' ';

        if (item.kind === 'profile') {
          const p = item.profile;
          const name = i === selected
            ? `\x1B[1m\x1B[33m${p.name}\x1B[0m`
            : `\x1B[37m${p.name}\x1B[0m`;
          const desc = p.description ? `  \x1B[90m${p.description}\x1B[0m` : '';
          console.log(`   ${cursor} ${i + 1}. ${name}${desc}`);

          if (i === selected && p.models) {
            const m = p.models;
            if (m.opus) console.log(`      \x1B[90m  opus   → ${m.opus}\x1B[0m`);
            if (m.sonnet) console.log(`      \x1B[90m  sonnet → ${m.sonnet}\x1B[0m`);
            if (m.haiku) console.log(`      \x1B[90m  haiku  → ${m.haiku}\x1B[0m`);
          }
        } else {
          const label = i === selected
            ? `\x1B[1m\x1B[32m+ 새 프로바이더 추가...\x1B[0m`
            : `\x1B[32m+ 새 프로바이더 추가...\x1B[0m`;
          console.log(`   ${cursor} ${i + 1}. ${label}`);
        }
      });

      console.log('');
      console.log('  \x1B[90m ↑↓ 이동  Enter 선택  e 편집  d 삭제  Esc 취소\x1B[0m');
      console.log('');
    }

    function finish(action) {
      stdin.setRawMode(false);
      stdin.pause();
      stdin.removeAllListeners('data');
      process.stdout.write('\x1B[2J\x1B[H');
      resolve(action);
    }

    function pickAtIndex(idx) {
      const item = items[idx];
      if (item.kind === 'profile') {
        finish({ kind: 'launch', profile: item.profile });
      } else {
        finish({ kind: 'add' });
      }
    }

    render();

    stdin.on('data', (key) => {
      // Escape or Ctrl+C
      if (key === '\x1B' || key === '\x03') {
        stdin.setRawMode(false);
        stdin.pause();
        process.stdout.write('\x1B[2J\x1B[H');
        console.log('취소되었습니다.');
        process.exit(0);
      }

      // Enter
      if (key === '\r' || key === '\n') {
        pickAtIndex(selected);
        return;
      }

      // Up arrow
      if (key === '\x1B[A' || key === '\x1BOA') {
        selected = Math.max(0, selected - 1);
        render();
        return;
      }

      // Down arrow
      if (key === '\x1B[B' || key === '\x1BOB') {
        selected = Math.min(items.length - 1, selected + 1);
        render();
        return;
      }

      // 편집 (e) / 삭제 (d) — 포커스가 실제 프로파일일 때만
      if (key === 'e' || key === 'E') {
        const item = items[selected];
        if (item.kind === 'profile') {
          finish({ kind: 'edit', index: item.index, profile: item.profile });
        }
        return;
      }
      if (key === 'd' || key === 'D') {
        const item = items[selected];
        if (item.kind === 'profile') {
          finish({ kind: 'delete', index: item.index, profile: item.profile });
        }
        return;
      }

      // Number keys 1-9
      if (key >= '1' && key <= '9') {
        const idx = parseInt(key) - 1;
        if (idx < items.length) {
          pickAtIndex(idx);
          return;
        }
      }
    });
  });
}

// ── Line-oriented Prompts (for add flow) ────────────────────────

// prefill=true면 기본값을 입력 버퍼에 미리 채워 편집 가능 상태로 표시한다.
// prefill=false면 기존 동작: `[default]` 힌트만 보이고 Enter로 수락.
function promptLine(question, { default: def = '', required = true, prefill = false } = {}) {
  return new Promise((resolve) => {
    const rl = createInterface({ input: process.stdin, output: process.stdout });
    const hint = !prefill && def ? ` \x1B[90m[${def}]\x1B[0m` : '';
    rl.question(`  ${question}${hint}: `, (answer) => {
      rl.close();
      const v = prefill ? answer.trim() : (answer.trim() || def);
      if (required && !v) {
        console.log('  \x1B[31m값이 필요합니다.\x1B[0m');
        resolve(promptLine(question, { default: def, required, prefill }));
      } else {
        resolve(v);
      }
    });
    if (prefill && def) {
      rl.write(def);
    }
  });
}

async function promptChoice(question, choices) {
  console.log('');
  console.log(`  \x1B[1m${question}\x1B[0m`);
  choices.forEach((c, i) => console.log(`    ${i + 1}. ${c}`));
  while (true) {
    const ans = await promptLine('번호', { required: true });
    const idx = parseInt(ans) - 1;
    if (idx >= 0 && idx < choices.length) return idx;
    console.log('  \x1B[31m유효하지 않은 번호입니다.\x1B[0m');
  }
}

// ── Searchable Provider Catalog ─────────────────────────────────

// items: [{ label, description?, payload, pinned? }]
// pinned 항목은 검색어와 무관하게 항상 목록 하단에 표시
// 반환: 선택된 item.payload, 또는 null (Esc 취소)
async function selectFromCatalog(items, title) {
  return new Promise((resolve) => {
    const stdin = process.stdin;
    if (!stdin.isTTY) {
      console.error('Error: 인터랙티브 모드에는 TTY가 필요합니다.');
      process.exit(1);
    }

    let query = '';
    let selected = 0;

    // 검색 매칭: 대소문자/공백 무시 substring
    // "lmst"가 "LM Studio"에 매칭되도록 양쪽 모두 소문자화 + 공백 제거
    function normalize(s) {
      return (s || '').toLowerCase().replace(/\s+/g, '');
    }

    function getVisible() {
      if (!query) return items.slice();
      const q = normalize(query);
      return items.filter((it) =>
        it.pinned ||
        normalize(it.label).includes(q) ||
        normalize(it.description).includes(q)
      );
    }

    stdin.setRawMode(true);
    stdin.resume();
    stdin.setEncoding('utf-8');

    function render() {
      const visible = getVisible();
      const matched = visible.filter((v) => !v.pinned).length;
      const totalBase = items.filter((v) => !v.pinned).length;

      process.stdout.write('\x1B[2J\x1B[H');
      console.log('');
      console.log(`  \x1B[1m\x1B[36m claudex \x1B[0m\x1B[90m— ${title}\x1B[0m`);
      console.log('');

      // 검색창
      const counter = query ? `\x1B[90m  (${matched}/${totalBase})\x1B[0m` : '';
      const placeholder = query ? '' : '\x1B[90m타이핑하여 검색\x1B[0m';
      console.log(`  \x1B[36m검색:\x1B[0m \x1B[1m${query}\x1B[0m\x1B[7m \x1B[0m${placeholder}${counter}`);
      console.log('  \x1B[90m' + '─'.repeat(56) + '\x1B[0m');
      console.log('');

      if (visible.length === 0) {
        console.log('   \x1B[90m(일치하는 항목 없음)\x1B[0m');
      } else {
        visible.forEach((item, i) => {
          const cursor = i === selected ? '\x1B[33m❯\x1B[0m' : ' ';
          const color = item.pinned ? '32' : '37';
          const label = i === selected
            ? `\x1B[1m\x1B[33m${item.label}\x1B[0m`
            : `\x1B[${color}m${item.label}\x1B[0m`;
          const desc = item.description ? `  \x1B[90m${item.description}\x1B[0m` : '';
          console.log(`   ${cursor} ${label}${desc}`);
        });
      }

      console.log('');
      console.log('  \x1B[90m 문자 입력: 검색  ↑↓: 이동  Enter: 선택  Backspace: 지우기  Esc: 취소\x1B[0m');
      console.log('');
    }

    function cleanup() {
      stdin.setRawMode(false);
      stdin.pause();
      stdin.removeAllListeners('data');
    }

    render();

    stdin.on('data', (chunk) => {
      // 단일 chunk로 오는 메타키 먼저 정확 매칭
      if (chunk === '\x03') {
        cleanup();
        process.stdout.write('\x1B[2J\x1B[H');
        console.log('취소되었습니다.');
        process.exit(0);
      }
      if (chunk === '\x1B[A' || chunk === '\x1BOA') {
        selected = Math.max(0, selected - 1);
        render();
        return;
      }
      if (chunk === '\x1B[B' || chunk === '\x1BOB') {
        const visible = getVisible();
        selected = Math.min(Math.max(0, visible.length - 1), selected + 1);
        render();
        return;
      }
      if (chunk === '\x1B[C' || chunk === '\x1BOC' || chunk === '\x1B[D' || chunk === '\x1BOD') {
        return;
      }
      if (chunk === '\x1B') {
        cleanup();
        process.stdout.write('\x1B[2J\x1B[H');
        resolve(null);
        return;
      }
      if (chunk === '\r' || chunk === '\n') {
        const visible = getVisible();
        if (visible[selected]) {
          cleanup();
          process.stdout.write('\x1B[2J\x1B[H');
          resolve(visible[selected].payload);
        }
        return;
      }
      if (chunk.startsWith('\x1B')) return; // 기타 ESC 시퀀스 무시

      // 텍스트 입력: 코드포인트 단위로 순회하며 backspace/인쇄문자 처리
      // (paste, 빠른 연타, 한글 입력 등 복합 chunk 대응)
      let changed = false;
      for (const ch of chunk) {
        const code = ch.charCodeAt(0);
        if (ch === '\x7F' || ch === '\b') {
          if (query.length > 0) {
            query = query.slice(0, -1);
            changed = true;
          }
        } else if (code >= 0x20) {
          query += ch;
          changed = true;
        }
      }
      if (changed) {
        selected = 0;
        render();
      }
    });
  });
}

// ── Add Provider Flow ───────────────────────────────────────────

async function addProfileFlow(config) {
  const existing = config.profiles || [];

  // 카탈로그 로드 (example.json의 프로파일들)
  const examplePath = join(__dirname, EXAMPLE_CONFIG);
  let templates = [];
  if (existsSync(examplePath)) {
    try {
      templates = JSON.parse(readFileSync(examplePath, 'utf-8')).profiles || [];
    } catch (e) {
      console.log(`  \x1B[31m카탈로그 파싱 실패: ${e.message}\x1B[0m`);
    }
  }

  const items = [
    ...templates.map((t) => ({
      label: t.name,
      description: t.description || '',
      payload: { kind: 'template', data: t },
    })),
    {
      label: '기타 (직접 입력)',
      description: '카탈로그에 없는 프로바이더를 수동으로 추가',
      payload: { kind: 'manual' },
      pinned: true,
    },
  ];

  const picked = await selectFromCatalog(items, '새 프로바이더 추가');
  if (!picked) return; // Esc 취소

  const newProfile = picked.kind === 'template'
    ? await customizeTemplate(picked.data, existing)
    : await addManual(existing);

  if (!newProfile) return;

  const nextProfiles = [...existing, newProfile];
  try {
    writeFileSync(
      config._configPath,
      JSON.stringify({ profiles: nextProfiles }, null, 2) + '\n',
      'utf-8'
    );
    console.log('');
    console.log(`  \x1B[32m✓\x1B[0m "${newProfile.name}" 저장됨`);
    console.log(`  \x1B[90m${config._configPath}\x1B[0m`);
  } catch (e) {
    console.log('');
    console.log(`  \x1B[31m저장 실패: ${e.message}\x1B[0m`);
  }
  console.log('');
  await promptLine('Enter를 눌러 계속', { required: false });
}

async function customizeTemplate(template, existing) {
  const tpl = JSON.parse(JSON.stringify(template));
  const existingNames = new Set(existing.map((p) => p.name.toLowerCase()));
  const isLMStudio = /LM\s*Studio/i.test(template.name);
  const isOpenRouter = isOpenRouterProfile(template);

  console.log('');
  console.log(`  \x1B[36m[${template.name}]\x1B[0m 설정`);
  console.log('');

  // 이름 — prefill 기본값
  let suggestedName = tpl.name;
  if (existingNames.has(suggestedName.toLowerCase())) {
    suggestedName = `${tpl.name} (copy)`;
  }
  let name = await promptLine('프로파일 이름', { default: suggestedName, prefill: true });
  while (existingNames.has(name.toLowerCase())) {
    console.log(`  \x1B[31m"${name}"은 이미 존재합니다.\x1B[0m`);
    name = await promptLine('프로파일 이름', { default: suggestedName, prefill: true });
  }
  tpl.name = name;

  // LM Studio는 사용자별 로컬 서버 주소가 다를 수 있어 baseUrl을 먼저 물어봄
  if (isLMStudio) {
    tpl.baseUrl = await promptLine('baseUrl (엔드포인트)', { default: tpl.baseUrl, prefill: true });
  }

  // 인증 — prefill
  if (tpl.authToken !== undefined) {
    tpl.authToken = await promptLine('authToken (Bearer 토큰)', { default: tpl.authToken, prefill: true });
  }
  if (tpl.apiKey !== undefined) {
    tpl.apiKey = await promptLine('apiKey', { default: tpl.apiKey, prefill: true });
  }

  // LM Studio: 실제 로드된 모델 목록 조회해서 등록
  if (isLMStudio) {
    await configureLMStudioModels(tpl);
  } else if (isOpenRouter) {
    await configureOpenRouterModels(tpl);
  }

  return tpl;
}

function isOpenRouterProfile(p) {
  return /openrouter/i.test(p?.name || '') || /openrouter\.ai/i.test(p?.baseUrl || '');
}

async function fetchOpenAIModels(baseUrl, token) {
  // LM Studio는 OpenAI 호환 /v1/models 제공
  const base = baseUrl.replace(/\/+$/, '').replace(/\/v1$/, '');
  const url = `${base}/v1/models`;
  const headers = { Accept: 'application/json' };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  try {
    const res = await fetch(url, { headers, signal: AbortSignal.timeout(3000) });
    if (!res.ok) return { error: `HTTP ${res.status} ${res.statusText}` };
    const data = await res.json();
    const list = Array.isArray(data?.data)
      ? data.data.map((m) => m?.id).filter(Boolean)
      : [];
    return { models: list, url };
  } catch (e) {
    return { error: e.message || String(e), url };
  }
}

async function configureLMStudioModels(tpl) {
  console.log('');
  console.log(`  \x1B[36m[claudex]\x1B[0m 모델 목록 조회 중... \x1B[90m(${tpl.baseUrl}/v1/models)\x1B[0m`);
  const result = await fetchOpenAIModels(tpl.baseUrl, tpl.authToken);

  if (result.error) {
    console.log(`  \x1B[33m⚠ 조회 실패: ${result.error}\x1B[0m`);
    console.log('  \x1B[90m모델을 수동으로 입력하세요.\x1B[0m');
    tpl.models = await promptModelsManual(tpl.models || {});
    return;
  }

  if (result.models.length === 0) {
    console.log('  \x1B[33m⚠ 로드된 모델이 없습니다. LM Studio에서 모델을 먼저 로드하세요.\x1B[0m');
    tpl.models = await promptModelsManual(tpl.models || {});
    return;
  }

  console.log(`  \x1B[32m✓\x1B[0m ${result.models.length}개 모델 발견`);

  // 모델이 1개면 자동 선택, 여러 개면 카탈로그 UI로 선택
  let chosen;
  if (result.models.length === 1) {
    chosen = result.models[0];
    console.log(`  \x1B[32m✓\x1B[0m 자동 선택: ${chosen}`);
  } else {
    const items = result.models.map((m) => ({ label: m, payload: m }));
    chosen = await selectFromCatalog(items, 'LM Studio 모델 선택 (모든 티어에 적용)');
    if (!chosen) {
      // Esc로 취소 → 첫 번째 모델로 폴백
      chosen = result.models[0];
      console.log(`  \x1B[90m취소됨 — 기본값 사용: ${chosen}\x1B[0m`);
    }
  }

  tpl.models = { opus: chosen, sonnet: chosen, haiku: chosen };
}

async function fetchOpenRouterModels(token) {
  const url = 'https://openrouter.ai/api/v1/models';
  const headers = { Accept: 'application/json' };
  // /models 엔드포인트는 공개지만 토큰이 있으면 같이 보낸다 (rate limit 완화)
  if (token) headers['Authorization'] = `Bearer ${token}`;
  try {
    const res = await fetch(url, { headers, signal: AbortSignal.timeout(10000) });
    if (!res.ok) return { error: `HTTP ${res.status} ${res.statusText}` };
    const data = await res.json();
    const list = Array.isArray(data?.data) ? data.data : [];
    return { models: list };
  } catch (e) {
    return { error: e.message || String(e) };
  }
}

function formatOpenRouterModelDesc(m) {
  const parts = [];
  if (m.context_length) {
    const k = Math.round(m.context_length / 1000);
    parts.push(`ctx ${k}k`);
  }
  // pricing 단위는 토큰당 USD 문자열. 1M 토큰 단가로 환산해 표시.
  const pIn = parseFloat(m.pricing?.prompt);
  const pOut = parseFloat(m.pricing?.completion);
  if (Number.isFinite(pIn) && Number.isFinite(pOut) && (pIn > 0 || pOut > 0)) {
    parts.push(`$${(pIn * 1_000_000).toFixed(2)}/$${(pOut * 1_000_000).toFixed(2)} per 1M`);
  } else if (pIn === 0 && pOut === 0) {
    parts.push('free');
  }
  return parts.join(' · ');
}

async function configureOpenRouterModels(tpl) {
  console.log('');
  console.log(`  \x1B[36m[claudex]\x1B[0m OpenRouter 모델 목록 조회 중... \x1B[90m(https://openrouter.ai/api/v1/models)\x1B[0m`);
  const result = await fetchOpenRouterModels(tpl.apiKey || tpl.authToken);

  if (result.error || !result.models?.length) {
    if (result.error) console.log(`  \x1B[33m⚠ 조회 실패: ${result.error}\x1B[0m`);
    else console.log('  \x1B[33m⚠ 모델 목록이 비어 있습니다.\x1B[0m');
    console.log('  \x1B[90m모델을 수동으로 입력하세요.\x1B[0m');
    tpl.models = await promptModelsManual(tpl.models || {});
    return;
  }

  console.log(`  \x1B[32m✓\x1B[0m ${result.models.length}개 모델 발견`);

  const baseItems = result.models.map((m) => ({
    label: m.id,
    description: formatOpenRouterModelDesc(m),
    payload: m.id,
  }));
  const skipItem = {
    label: '(이 티어는 설정하지 않음)',
    description: '환경변수 미지정 — Claude Code 기본 동작',
    payload: '',
    pinned: true,
  };

  const current = { ...(tpl.models || {}) };
  const next = {};
  for (const tier of ['opus', 'sonnet', 'haiku']) {
    const title = `${tier} 티어 모델 선택${current[tier] ? ` — 현재: ${current[tier]}` : ''}`;
    const picked = await selectFromCatalog([...baseItems, skipItem], title);
    if (picked === null) {
      // Esc: 기존 값 유지
      if (current[tier]) next[tier] = current[tier];
    } else if (picked) {
      next[tier] = picked;
    }
    // picked === '' (skip 항목): 해당 티어 미설정
  }

  tpl.models = Object.keys(next).length ? next : undefined;
}

async function promptModelsManual(current) {
  const opus = await promptLine('모델 opus', {
    default: current.opus || '',
    required: false,
    prefill: true,
  });
  const sonnet = await promptLine('모델 sonnet', {
    default: current.sonnet || opus,
    required: false,
    prefill: true,
  });
  const haiku = await promptLine('모델 haiku', {
    default: current.haiku || sonnet || opus,
    required: false,
    prefill: true,
  });
  const models = {};
  if (opus) models.opus = opus;
  if (sonnet) models.sonnet = sonnet;
  if (haiku) models.haiku = haiku;
  return Object.keys(models).length ? models : undefined;
}

async function addManual(existing) {
  console.log('');
  console.log('  \x1B[90m필드를 하나씩 입력합니다. Ctrl+C로 취소.\x1B[0m');

  const existingNames = new Set(existing.map((p) => p.name.toLowerCase()));
  let name = await promptLine('프로파일 이름');
  while (existingNames.has(name.toLowerCase())) {
    console.log(`  \x1B[31m"${name}"은 이미 존재합니다.\x1B[0m`);
    name = await promptLine('프로파일 이름');
  }

  const description = await promptLine('설명 (선택)', { required: false });
  const baseUrl = await promptLine('baseUrl (예: https://api.example.com/anthropic)');

  const authType = await promptChoice('인증 방식', [
    'authToken — Authorization: Bearer 헤더 (z.ai, Kimi, Ollama 등)',
    'apiKey — x-api-key 헤더 (DeepSeek, MiniMax, OpenRouter 등)',
  ]);
  const authField = authType === 0 ? 'authToken' : 'apiKey';
  const authValue = await promptLine(`${authField} 값`);

  const opus = await promptLine('모델 opus (선택)', { required: false });
  const sonnet = await promptLine('모델 sonnet (선택)', { required: false, default: opus });
  const haiku = await promptLine('모델 haiku (선택)', { required: false, default: sonnet || opus });

  const profile = { name, baseUrl, [authField]: authValue };
  if (description) profile.description = description;
  const models = {};
  if (opus) models.opus = opus;
  if (sonnet) models.sonnet = sonnet;
  if (haiku) models.haiku = haiku;
  if (Object.keys(models).length) profile.models = models;

  return profile;
}

// ── Edit / Delete Flows ─────────────────────────────────────────

async function editProfileFlow(config, index) {
  const existing = config.profiles || [];
  const original = existing[index];
  if (!original) return;

  const edited = JSON.parse(JSON.stringify(original));
  const otherNames = new Set(
    existing.filter((_, i) => i !== index).map((p) => p.name.toLowerCase())
  );
  const isLMStudio = /LM\s*Studio/i.test(original.name);
  const isOpenRouter = isOpenRouterProfile(original) || isOpenRouterProfile(edited);

  process.stdout.write('\x1B[2J\x1B[H');
  console.log('');
  console.log(`  \x1B[1m\x1B[36m claudex \x1B[0m\x1B[90m— 프로파일 편집: ${original.name}\x1B[0m`);
  console.log('  \x1B[90mEnter로 기존 값 유지, Ctrl+U로 지우고 재입력\x1B[0m');
  console.log('');

  // 이름 — 자기 자신 제외 중복 체크
  let name = await promptLine('프로파일 이름', { default: edited.name, prefill: true });
  while (otherNames.has(name.toLowerCase())) {
    console.log(`  \x1B[31m"${name}"은 다른 프로파일에서 사용 중입니다.\x1B[0m`);
    name = await promptLine('프로파일 이름', { default: edited.name, prefill: true });
  }
  edited.name = name;

  // baseUrl (모든 프로바이더에서 편집 가능)
  if (edited.baseUrl !== undefined) {
    edited.baseUrl = await promptLine('baseUrl', { default: edited.baseUrl, prefill: true });
  }

  // 인증 — 기존에 있던 필드만 prefill
  if (edited.authToken !== undefined) {
    edited.authToken = await promptLine('authToken (Bearer 토큰)', {
      default: edited.authToken,
      prefill: true,
    });
  }
  if (edited.apiKey !== undefined) {
    edited.apiKey = await promptLine('apiKey', { default: edited.apiKey, prefill: true });
  }

  // 모델
  if (isLMStudio) {
    const ans = await promptLine('모델 목록을 다시 조회할까요? (y/N)', { required: false });
    if (ans.trim().toLowerCase() === 'y') {
      await configureLMStudioModels(edited);
    } else {
      edited.models = await promptModelsManual(edited.models || {});
    }
  } else if (isOpenRouter) {
    const ans = await promptLine('OpenRouter 모델 목록을 다시 조회할까요? (y/N)', { required: false });
    if (ans.trim().toLowerCase() === 'y') {
      await configureOpenRouterModels(edited);
    } else {
      edited.models = await promptModelsManual(edited.models || {});
    }
  } else {
    edited.models = await promptModelsManual(edited.models || {});
  }

  // undefined 정리 — promptModelsManual이 빈 객체를 undefined로 반환하는 경우 삭제
  if (edited.models === undefined) delete edited.models;

  existing[index] = edited;
  try {
    writeFileSync(
      config._configPath,
      JSON.stringify({ profiles: existing }, null, 2) + '\n',
      'utf-8'
    );
    console.log('');
    console.log(`  \x1B[32m✓\x1B[0m "${edited.name}" 수정됨`);
    console.log(`  \x1B[90m${config._configPath}\x1B[0m`);
  } catch (e) {
    console.log('');
    console.log(`  \x1B[31m저장 실패: ${e.message}\x1B[0m`);
  }
  console.log('');
  await promptLine('Enter를 눌러 계속', { required: false });
}

async function deleteProfileFlow(config, index) {
  const existing = config.profiles || [];
  const target = existing[index];
  if (!target) return;

  process.stdout.write('\x1B[2J\x1B[H');
  console.log('');
  console.log(`  \x1B[1m\x1B[31m claudex \x1B[0m\x1B[90m— 프로파일 삭제\x1B[0m`);
  console.log('');
  console.log(`  이름:    \x1B[1m${target.name}\x1B[0m`);
  if (target.baseUrl) console.log(`  baseUrl: \x1B[90m${target.baseUrl}\x1B[0m`);
  if (target.description) console.log(`  설명:    \x1B[90m${target.description}\x1B[0m`);
  console.log('');
  console.log('  \x1B[33m⚠ 이 작업은 되돌릴 수 없습니다.\x1B[0m');
  console.log('');

  const ans = await promptLine('삭제하려면 y 입력, 취소는 Enter', { required: false });
  if (ans.trim().toLowerCase() !== 'y') {
    console.log('  \x1B[90m취소되었습니다.\x1B[0m');
    console.log('');
    await promptLine('Enter를 눌러 계속', { required: false });
    return;
  }

  existing.splice(index, 1);
  try {
    writeFileSync(
      config._configPath,
      JSON.stringify({ profiles: existing }, null, 2) + '\n',
      'utf-8'
    );
    console.log('');
    console.log(`  \x1B[32m✓\x1B[0m "${target.name}" 삭제됨`);
  } catch (e) {
    console.log('');
    console.log(`  \x1B[31m삭제 실패: ${e.message}\x1B[0m`);
  }
  console.log('');
  await promptLine('Enter를 눌러 계속', { required: false });
}

// ── Claude Launcher ─────────────────────────────────────────────

function launchClaude(profile, claudeArgs) {
  const env = { ...process.env };

  if (profile.baseUrl) {
    env.ANTHROPIC_BASE_URL = profile.baseUrl;
  }
  if (profile.apiKey) {
    env.ANTHROPIC_API_KEY = profile.apiKey;
  }
  if (profile.authToken) {
    env.ANTHROPIC_AUTH_TOKEN = profile.authToken;
  }
  if (profile.models?.opus) {
    env.ANTHROPIC_DEFAULT_OPUS_MODEL = profile.models.opus;
  }
  if (profile.models?.sonnet) {
    env.ANTHROPIC_DEFAULT_SONNET_MODEL = profile.models.sonnet;
  }
  if (profile.models?.haiku) {
    env.ANTHROPIC_DEFAULT_HAIKU_MODEL = profile.models.haiku;
  }
  if (profile.model) {
    env.ANTHROPIC_MODEL = profile.model;
  }
  // 추가 환경변수 (API_TIMEOUT_MS 등)
  if (profile.env) {
    for (const [key, value] of Object.entries(profile.env)) {
      env[key] = String(value);
    }
  }

  // 프로파일 정보 표시
  console.log(`\x1B[36m[claudex]\x1B[0m 프로파일: \x1B[1m${profile.name}\x1B[0m`);
  if (profile.baseUrl) {
    console.log(`\x1B[36m[claudex]\x1B[0m API: ${profile.baseUrl}`);
  }
  if (profile.models) {
    const m = profile.models;
    const parts = [];
    if (m.opus) parts.push(`opus→${m.opus}`);
    if (m.sonnet) parts.push(`sonnet→${m.sonnet}`);
    if (m.haiku) parts.push(`haiku→${m.haiku}`);
    if (parts.length) {
      console.log(`\x1B[36m[claudex]\x1B[0m 모델: ${parts.join(', ')}`);
    }
  }
  console.log('');

  const child = spawn(CLAUDE_CMD, claudeArgs, {
    env,
    stdio: 'inherit',
    shell: true,
  });

  child.on('error', (err) => {
    if (err.code === 'ENOENT') {
      console.error('Error: "claude"를 찾을 수 없습니다. Claude Code가 설치되어 있나요?');
      console.error('설치: https://docs.anthropic.com/en/docs/claude-code');
    } else {
      console.error(`claude 실행 오류: ${err.message}`);
    }
    process.exit(1);
  });

  child.on('exit', (code) => {
    process.exit(code ?? 1);
  });
}

// ── Main ────────────────────────────────────────────────────────

async function main() {
  let config = loadConfig();
  const { profileName, claudeArgs } = parseArgs();

  // -xSet으로 직접 지정: 설정 필수
  if (profileName) {
    if (config._missing) {
      console.error(`Error: 설정 파일을 찾을 수 없습니다: ${config._configPath}`);
      process.exit(1);
    }
    const profile = config.profiles.find(
      (p) => p.name.toLowerCase() === profileName.toLowerCase()
    );
    if (!profile) {
      console.error(`Error: 프로파일 "${profileName}"을(를) 찾을 수 없습니다.`);
      console.error('');
      console.error('사용 가능한 프로파일:');
      config.profiles.forEach((p) => {
        console.error(`  - ${p.name}${p.description ? ` (${p.description})` : ''}`);
      });
      process.exit(1);
    }
    launchClaude(profile, claudeArgs);
    return;
  }

  // 인터랙티브 메뉴 — 사용자가 프로파일을 고를 때까지 루프
  // config가 없으면 addProfileFlow가 파일을 생성한다
  while (true) {
    const action = await selectProfile(config.profiles || []);
    if (action.kind === 'launch') {
      launchClaude(action.profile, claudeArgs);
      return;
    }
    if (action.kind === 'add') {
      await addProfileFlow(config);
    } else if (action.kind === 'edit') {
      await editProfileFlow(config, action.index);
    } else if (action.kind === 'delete') {
      await deleteProfileFlow(config, action.index);
    }
    config = loadConfig(); // 저장 결과를 반영해 재로드
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
