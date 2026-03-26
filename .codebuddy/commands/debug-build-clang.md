---
description: Build the project in debug mode
auto_execute: true
---

```bash
cd /data/workspace_vscode/fast_web_meta_crawler && go build -gcflags="all=-N -l" -o build/crawler ./src/...
```