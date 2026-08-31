#!/usr/bin/env python3
"""template.html 의 {{IMG_*}} 자리에 스크린샷을 data URI로 넣어 index.html 을 만든다."""
import base64, pathlib, re, sys

HERE = pathlib.Path(__file__).parent
SHOTS = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else '/tmp/shots')
NAMES = {'DASHBOARD': 'dashboard', 'APPDETAIL': 'appdetail', 'PROXY': 'proxy', 'NOTIFY': 'notify'}

doc = (HERE / 'template.html').read_text(encoding='utf-8')
for token, shot in NAMES.items():
    path = SHOTS / f's-{shot}.png'
    if not path.exists():
        sys.exit(f'missing screenshot: {path}')
    uri = 'data:image/png;base64,' + base64.b64encode(path.read_bytes()).decode()
    doc = doc.replace('{{IMG_%s}}' % token, uri)

left = re.findall(r'\{\{[A-Z_]+\}\}', doc)
if left:
    sys.exit(f'unresolved placeholders: {sorted(set(left))}')

out = HERE / 'index.html'
out.write_text(doc, encoding='utf-8')
print(f'{out} — {out.stat().st_size / 1024:.0f} KB')
