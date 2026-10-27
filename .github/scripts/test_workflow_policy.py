import copy
import pathlib
import re
import json
import unittest
import yaml
from check_workflows import validate


class PolicyTests(unittest.TestCase):
    def setUp(self):
        self.good = {'permissions': {'contents': 'read'}, 'jobs': {'publish': {
            'permissions': {'contents': 'write'}, 'steps': [
                {'uses': 'actions/checkout@' + 'a' * 40,
                 'with': {'persist-credentials': False}},
                {'uses': 'owner/action@' + 'b' * 40,
                 'env': {'TOKEN': '${{ secrets.GITHUB_TOKEN }}'}},
            ]}}}

    def test_pinned_step_scoped_publisher(self):
        self.assertEqual([], validate(self.good))

    def test_local_reusable_workflow(self):
        self.good['jobs']['checks'] = {'uses': './.github/workflows/pr.yaml'}
        self.assertEqual([], validate(self.good))

    def test_mutable_and_malformed_refs(self):
        for ref in ['owner/action@v1.2.3', 'owner/action@main', 'owner/action@' + 'a'*39,
                    'owner/action@' + 'z'*40, 'docker://alpine:latest', '${{ inputs.action }}']:
            with self.subTest(ref=ref):
                doc = copy.deepcopy(self.good)
                doc['jobs']['publish']['steps'][1]['uses'] = ref
                self.assertTrue(validate(doc))

    def test_reusable_external_workflow_requires_sha(self):
        self.good['jobs']['checks'] = {'uses': 'owner/repo/.github/workflows/test.yml@main'}
        self.assertTrue(validate(self.good))

    def test_flow_and_quoted_yaml_cannot_hide_mutable_ref(self):
        doc = yaml.safe_load('permissions: {contents: read}\njobs: {test: {steps: [{"uses": "owner/action@v1"}]}}')
        self.assertTrue(validate(doc))

    def test_secrets_cannot_leak_to_checkout_via_env(self):
        for level in ['workflow', 'job']:
            doc = copy.deepcopy(self.good)
            target = doc if level == 'workflow' else doc['jobs']['publish']
            target['env'] = {'PASSWORD': '${{ secrets.DOCKER_PASSWORD }}'}
            self.assertTrue(validate(doc))

    def test_persisted_checkout_credentials_rejected(self):
        for value in [True, 'false', None]:
            doc = copy.deepcopy(self.good)
            doc['jobs']['publish']['steps'][0]['with']['persist-credentials'] = value
            self.assertTrue(validate(doc))

    def test_global_write_permissions_rejected(self):
        self.good['permissions'] = {'contents': 'write'}
        self.assertTrue(validate(self.good))

    def test_secret_in_validation_rejected(self):
        self.good['jobs']['test'] = {'steps': [{'run': 'test', 'env': {'TOKEN': '${{ secrets.TOKEN }}'}}]}
        self.assertTrue(validate(self.good))


class RepositoryGuards(unittest.TestCase):
    root = pathlib.Path(__file__).resolve().parents[2]

    def test_recorded_upstream_versions_and_sha_comments(self):
        pins = json.loads((self.root / '.github/action-pins.json').read_text())
        for path in (self.root / '.github/workflows').glob('*.yaml'):
            for repo, sha, version in re.findall(r'uses: ([^@\s]+)@([a-f0-9]{40}) # (v[^\s]+)', path.read_text()):
                self.assertEqual(pins[repo + '@' + version], sha)
            for line in path.read_text().splitlines():
                if 'uses:' in line and '@' in line:
                    self.assertRegex(line, r'@[a-f0-9]{40} # v[^\s]+$')

    def test_pr_has_no_private_credentials(self):
        text = (self.root / '.github/workflows/pr.yaml').read_text()
        self.assertNotIn('secrets.', text)
        self.assertNotIn('GOPRIVATE', text)

    def test_release_waits_for_checks(self):
        doc = yaml.safe_load((self.root / '.github/workflows/release.yaml').read_text())
        self.assertEqual('checks', doc['jobs']['release']['needs'])
        self.assertEqual({'contents': 'write'}, doc['jobs']['release']['permissions'])

    def test_description_credentials_only_reach_description_step(self):
        doc = yaml.safe_load((self.root / '.github/workflows/docker_hub_readme.yaml').read_text())
        self.assertNotIn('env', doc)
        steps = doc['jobs']['build_push']['steps']
        self.assertNotIn('secrets.', str(steps[0]))
        self.assertEqual(2, len(steps))
        self.assertTrue(steps[1]['uses'].startswith('peter-evans/dockerhub-description@'))

    def test_registry_write_is_only_in_publish_job(self):
        doc = yaml.safe_load((self.root / '.github/workflows/docker_release.yaml').read_text())
        self.assertEqual({'contents': 'read'}, doc['permissions'])
        self.assertNotIn('permissions', doc['jobs']['checks'])
        self.assertEqual({'contents': 'read', 'packages': 'write'}, doc['jobs']['publish']['permissions'])


if __name__ == '__main__':
    unittest.main()
