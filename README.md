# claudex

Claude Code를 z.ai GLM, Kimi, DeepSeek, MiniMax, OpenRouter, LM Studio 같은 다른 LLM 프로바이더로 돌려 쓰는 CLI 래퍼.

매번 환경변수를 세팅할 필요 없이, 프로파일을 골라서 `claude`를 실행합니다.

## 설치

```bash
git clone https://github.com/<your-username>/claudex.git
cd claudex
./build.sh
./install.sh
```

`~/.local/bin`이 `PATH`에 있어야 합니다. 없다면:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

사전 준비: [Claude Code](https://docs.claude.com/en/docs/claude-code) CLI가 설치돼 있어야 합니다.

## 사용법

**1. 설정 파일 만들기**

```bash
cp claudex.config.example.json claudex.config.json
```

**2. 사용할 프로바이더의 API 키 넣기**

`claudex.config.json`을 열어 쓰려는 프로바이더 항목의 `YOUR_..._API_KEY` 자리에 실제 키를 붙여넣습니다. 쓰지 않는 프로바이더는 그대로 둬도 됩니다.

**3. 실행**

```bash
claudex
```

메뉴에서 화살표 키로 프로파일을 고르면 해당 프로바이더로 연결된 `claude`가 뜹니다. 숫자 키로 바로 선택할 수도 있습니다.

프로파일을 미리 지정하려면:

```bash
claudex -xSet "GLM Coding Plan" -p "hello"
```

`-xSet` 외의 인자는 전부 `claude`에 그대로 전달됩니다. 예를 들어 권한 확인 프롬프트 없이 실행하려면:

```bash
claudex -xSet "GLM Coding Plan" --dangerously-skip-permissions
```

인터랙티브 메뉴와 함께도 쓸 수 있습니다 (메뉴에서 프로파일 선택 후 해당 옵션이 claude에 전달됨):

```bash
claudex --dangerously-skip-permissions
```

## 지원 프로바이더

z.ai GLM · Kimi (Moonshot) · DeepSeek · MiniMax · OpenRouter · LM Studio (로컬)

기본 설정은 `claudex.config.example.json`에 다 들어 있어 손댈 필요가 없습니다. API 키만 채우면 동작합니다.

## 알아두면 좋은 점

- **ChatGPT Plus / Codex 계정은 쓸 수 없습니다.** OpenAI는 Anthropic 호환 엔드포인트를 제공하지 않습니다.
- **LM Studio(로컬)는 모델에 따라 품질 편차가 큽니다.** 툴 사용이나 긴 컨텍스트를 제대로 지원하지 않는 모델이 많습니다.
- `claudex.config.json`은 `.gitignore`에 포함돼 있어 실수로 커밋되지 않습니다.

## License

MIT
