"""Start the signed release image on a GitHub runner and verify embedded assets."""

import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import subprocess
import time
from urllib.error import URLError
from urllib.parse import urlsplit
from urllib.request import build_opener, ProxyHandler


class EntryAssets(HTMLParser):
    """Collect local JS and CSS paths from the rendered document."""

    def __init__(self):
        super().__init__()
        self.paths = set()

    def handle_starttag(self, tag, attrs):
        fields = dict(attrs)
        url = fields.get('src') if tag == 'script' else (
            fields.get('href') if tag == 'link' and fields.get('rel') == 'stylesheet' else None
        )
        if url and url.startswith('/static/') and not urlsplit(url).netloc:
            self.paths.add(url)


directory = Path(__file__).resolve().parent
release = json.loads((directory / 'registry-verification.json').read_text(encoding='utf-8'))
image = release['image']
version = release['version']
assert os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
assert os.environ.get('GITHUB_REPOSITORY') == 'cao258190/new-api'
assert os.environ.get('GITHUB_REF') == 'refs/heads/codex/verify-release-20261121'
assert 'DOCKER_HOST' not in os.environ
architecture = {'x86_64': 'amd64', 'aarch64': 'arm64'}[subprocess.check_output(
    ['docker', 'info', '--format', '{{.Architecture}}'], text=True, encoding='utf-8',
).strip()]
platform = 'linux/' + architecture
assert architecture in release['platforms']
subprocess.run(['docker', 'pull', '--platform', platform, image], check=True)
container = subprocess.check_output([
    'docker', 'run', '--rm', '-d', '--pull', 'never', '--platform', platform,
    '-p', '127.0.0.1::3000', '--label', 'codex.release-smoke=' + version, image,
], text=True, encoding='utf-8').strip()

try:
    binding = subprocess.check_output(
        ['docker', 'port', container, '3000/tcp'], text=True, encoding='utf-8',
    ).strip()
    assert binding.startswith('127.0.0.1:') and int(binding.rsplit(':', 1)[1]) > 0
    base_url = 'http://' + binding
    open_local = build_opener(ProxyHandler({})).open
    deadline = time.monotonic() + 90
    while True:
        try:
            with open_local(base_url + '/api/status', timeout=4) as response:
                status = json.load(response)
                assert response.status == 200 and status['success']
                assert status['data']['version'] == version
            break
        except URLError:
            if time.monotonic() >= deadline:
                raise
            time.sleep(2)

    assets = EntryAssets()
    for path in ('/', '/image-studio'):
        with open_local(base_url + path, timeout=10) as response:
            html = response.read().decode('utf-8')
            assert response.status == 200 and '<html' in html.lower()
            assets.feed(html)
    assert any(path.endswith('.js') for path in assets.paths)
    assert any(path.endswith('.css') for path in assets.paths)
    checked = {}
    for path in sorted(assets.paths):
        with open_local(base_url + path, timeout=15) as response:
            content = response.read()
            assert response.status == 200 and content
            assert response.headers.get_content_type() != 'text/html'
            checked[path] = 'sha256:' + hashlib.sha256(content).hexdigest()
    app = json.loads(subprocess.check_output(['docker', 'inspect', container]))[0]
    assert app['State']['Running'] and app['RestartCount'] == 0
    assert app['Config']['Labels']['org.opencontainers.image.revision'] == release['source_sha']
    result = {
        'status': 'passed', 'version': version, 'image': image, 'source_sha': release['source_sha'],
        'platform': platform, 'environment': 'isolated GitHub-hosted runner',
        'checked_routes': ['/', '/image-studio', '/api/status'], 'checked_assets': checked,
        'restarts': app['RestartCount'],
    }
    (directory / 'image-smoke.json').write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8',
    )
    print(json.dumps(result, ensure_ascii=False))
finally:
    subprocess.run(['docker', 'stop', '--time', '5', container], check=True, stdout=subprocess.DEVNULL)
