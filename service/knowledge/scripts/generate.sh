#!/usr/bin/env bash
set -euo pipefail
knowledge_repo="$(cd "$(dirname "$0")/../../.." && pwd)"
knowledge_goctl="${KNOWLEDGE_GOCTL:-goctl}"
if [[ "$($knowledge_goctl --version)" != *"1.9.2"* ]]; then
  printf 'knowledge generation requires goctl 1.9.2\n' >&2
  exit 1
fi
cd "$knowledge_repo"
"$knowledge_goctl" api go -api api/knowledge.api -dir service/knowledge/api -style go_zero
"$knowledge_goctl" api swagger --api api/knowledge.api --dir service/knowledge/generated --filename knowledge
"$knowledge_goctl" api ts --api api/knowledge.api --dir service/knowledge/generated/typescript
# goctl 1.9.2 emits trailing spaces in empty descriptions. Normalize generated text reproducibly.
python3 - <<'PY'
from pathlib import Path
paths=[Path('api/knowledge.api'),*Path('service/knowledge/generated').rglob('*.ts')]
for path in paths:
    path.write_text('\n'.join(line.rstrip() for line in path.read_text().splitlines()).rstrip()+'\n')
PY
gofmt -w service/knowledge/api/internal/types/types.go service/knowledge/api/internal/handler/routes.go
