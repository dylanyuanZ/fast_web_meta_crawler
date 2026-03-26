---
description: Build the project in release mode
auto_execute: true
---

```bash
cd /data/workspace_vscode/fast_web_meta_crawler && go build -ldflags="-s -w" -o build/crawler ./src/...
```