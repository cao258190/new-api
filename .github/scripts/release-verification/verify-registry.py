"""Match the signed Actions artifact to both Docker Hub image platforms."""

from hashlib import sha256
import json
from pathlib import Path
import re
import sys
from urllib.parse import urlencode
from urllib.request import Request, urlopen


directory = Path(__file__).resolve().parent
version = 'custom-v2026.11.21.1'
repo = 'caozhihong123/new-api'
workflow = 'cao258190/new-api/.github/workflows/docker-build.yml@refs/tags/' + version
source = sys.argv[1]
assert re.fullmatch(r'[a-f0-9]{40}', source)

release = json.loads((directory / 'release.json').read_text(encoding='utf-8'))
assert release['version'] == version and release['source_sha'] == source
assert release['workflow'] == workflow and release['signature_verified'] is True
image = release['image']
assert re.fullmatch(re.escape(repo) + r'@sha256:[a-f0-9]{64}', image)
digest = image.split('@', 1)[1]
signatures = json.loads((directory / 'cosign-verification.json').read_text(encoding='utf-8'))
assert any(
    sig['critical']['image']['docker-manifest-digest'] == digest
    and sig['critical']['identity']['docker-reference'].endswith('/' + image)
    for sig in signatures
), 'Signed image digest must match the release'

query = urlencode({'service': 'registry.docker.io', 'scope': f'repository:{repo}:pull'})
with urlopen('https://auth.docker.io/token?' + query, timeout=20) as response:
    token = json.load(response)['token']
accept = ', '.join((
    'application/vnd.oci.image.index.v1+json',
    'application/vnd.oci.image.manifest.v1+json',
    'application/vnd.docker.distribution.manifest.list.v2+json',
    'application/vnd.docker.distribution.manifest.v2+json',
))


def read_registry_json(kind, ref):
    """Read and verify the digest of a public OCI document."""
    request = Request(f'https://registry-1.docker.io/v2/{repo}/{kind}/{ref}', headers={'Accept': accept})
    request.add_unredirected_header('Authorization', 'Bearer ' + token)
    with urlopen(request, timeout=30) as response:
        content = response.read()
        registry_digest = response.headers.get('Docker-Content-Digest')
    actual = 'sha256:' + sha256(content).hexdigest()
    if ref.startswith('sha256:'):
        assert actual == ref
    if kind == 'manifests':
        assert actual == registry_digest
    return json.loads(content), actual


index, actual_digest = read_registry_json('manifests', version)
assert digest == actual_digest
recorded_manifest = (directory / 'manifest.json').read_bytes()
assert json.loads(recorded_manifest) == index
assert 'sha256:' + sha256(recorded_manifest).hexdigest() == digest
assert sorted(
    entry['platform']['architecture']
    for entry in index['manifests']
    if entry.get('platform', {}).get('os') == 'linux'
) == ['amd64', 'arm64']

result = {
    **release,
    'signature_identity': 'https://github.com/' + workflow,
    'signature_issuer': 'https://token.actions.githubusercontent.com',
    'registry_digest_verified': True,
    'vulnerability_scan': 'not executed per repository policy',
    'platforms': {},
}

for arch in ('amd64', 'arm64'):
    descriptor = next(
        entry for entry in index['manifests']
        if entry.get('platform', {}).get('os') == 'linux'
        and entry['platform']['architecture'] == arch
    )
    platform_digest = descriptor['digest']
    arch_index, arch_digest = read_registry_json('manifests', f'{version}-{arch}')
    arch_children = {entry['digest'] for entry in arch_index['manifests']}
    assert platform_digest in arch_children
    manifest, _ = read_registry_json('manifests', platform_digest)
    config, _ = read_registry_json('blobs', manifest['config']['digest'])
    labels = config['config']['Labels']
    assert config['architecture'] == arch and config['os'] == 'linux'
    assert labels['org.opencontainers.image.revision'] == source
    assert labels['org.opencontainers.image.version'] == version

    attestations = [
        entry for entry in index['manifests']
        if entry.get('annotations', {}).get('vnd.docker.reference.digest') == platform_digest
        and entry['annotations'].get('vnd.docker.reference.type') == 'attestation-manifest'
    ]
    assert attestations
    predicates = set()
    for entry in attestations:
        assert entry['digest'] in arch_children
        attestation, _ = read_registry_json('manifests', entry['digest'])
        predicates.update(
            layer.get('annotations', {}).get('in-toto.io/predicate-type', '')
            for layer in attestation['layers'] if layer['mediaType'] == 'application/vnd.in-toto+json'
        )
    assert 'https://spdx.dev/Document' in predicates
    assert any(value.startswith('https://slsa.dev/provenance/') for value in predicates)
    result['platforms'][arch] = {
        'manifest_digest': platform_digest,
        'architecture_tag_digest': arch_digest,
        'attestation_digests': [entry['digest'] for entry in attestations],
        'predicate_types': sorted(predicates),
    }

(directory / 'registry-verification.json').write_text(
    json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8',
)
print(json.dumps(result, ensure_ascii=False, indent=2))
