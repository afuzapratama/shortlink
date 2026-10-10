#!/usr/bin/env bash
set -euo pipefail
project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
command -v php >/dev/null
command -v python3 >/dev/null
while IFS= read -r file; do php -l "$file" >/dev/null; done < <(rg --files "$project_root/services/agent-php" -g '*.php')
php "$project_root/services/agent-php/tests/unit.php"
python3 "$project_root/services/agent-php/tests/integration.py"
