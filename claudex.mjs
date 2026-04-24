#!/usr/bin/env node

import { readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

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

async function selectProfile(profiles) {
  if (profiles.length === 0) {
    const examplePath = join(__dirname, EXAMPLE_CONFIG);
    console.error('프로파일이 설정되지 않았습니다.');
    console.error(`\n${examplePath} 파일을 참고하여`);
    console.error(`${join(__dirname, CONFIG_FILENAME)} 파일을 생성하세요.`);
    process.exit(1);
  }

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

      profiles.forEach((p, i) => {
        const cursor = i === selected ? '\x1B[33m❯\x1B[0m' : ' ';
        const name = i === selected
          ? `\x1B[1m\x1B[33m${p.name}\x1B[0m`
          : `\x1B[37m${p.name}\x1B[0m`;
        const desc = p.description ? `  \x1B[90m${p.description}\x1B[0m` : '';

        console.log(`   ${cursor} ${i + 1}. ${name}${desc}`);

        if (i === selected && p.models) {
          const models = p.models;
          if (models.opus) console.log(`      \x1B[90m  opus   → ${models.opus}\x1B[0m`);
          if (models.sonnet) console.log(`      \x1B[90m  sonnet → ${models.sonnet}\x1B[0m`);
          if (models.haiku) console.log(`      \x1B[90m  haiku  → ${models.haiku}\x1B[0m`);
        }
      });

      console.log('');
      console.log('  \x1B[90m ↑↓ 이동  Enter 선택  Esc 취소\x1B[0m');
      console.log('');
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
        stdin.setRawMode(false);
        stdin.pause();
        process.stdout.write('\x1B[2J\x1B[H');
        resolve(profiles[selected]);
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
        selected = Math.min(profiles.length - 1, selected + 1);
        render();
        return;
      }

      // Number keys 1-9
      if (key >= '1' && key <= '9') {
        const idx = parseInt(key) - 1;
        if (idx < profiles.length) {
          stdin.setRawMode(false);
          stdin.pause();
          process.stdout.write('\x1B[2J\x1B[H');
          resolve(profiles[idx]);
          return;
        }
      }
    });
  });
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
  const config = loadConfig();
  const { profileName, claudeArgs } = parseArgs();

  // 설정 파일이 없고 -xSet도 없으면 안내 출력
  if (config._missing && !profileName) {
    const examplePath = join(__dirname, EXAMPLE_CONFIG);
    console.log('\x1B[36m[claudex]\x1B[0m 설정 파일이 없습니다.');
    console.log('');
    console.log(`  다음 파일을 복사하여 설정을 만드세요:`);
    console.log(`  \x1B[33m${examplePath}\x1B[0m`);
    console.log(`  → \x1B[33m${join(__dirname, CONFIG_FILENAME)}\x1B[0m`);
    console.log('');
    process.exit(1);
  }

  if (config._missing && profileName) {
    console.error(`Error: 설정 파일을 찾을 수 없습니다: ${config._configPath}`);
    process.exit(1);
  }

  let profile;

  if (profileName) {
    // -xSet으로 프로파일 직접 지정
    profile = config.profiles.find(
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
  } else {
    // 인터랙티브 메뉴
    profile = await selectProfile(config.profiles);
  }

  launchClaude(profile, claudeArgs);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
