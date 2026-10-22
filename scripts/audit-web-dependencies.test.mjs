import assert from 'node:assert/strict';
import test from 'node:test';
import { blockedFindings } from './audit-web-dependencies.mjs';

const url = 'https://github.com/advisories/GHSA-vfj7-8cjw-p6xm';
const report = (via = [{ url }]) => ({
  vulnerabilities: { braces: { severity: 'high', via, nodes: ['node_modules/braces'] } },
  metadata: { vulnerabilities: { high: 1 } },
});
const dev = { packages: { 'node_modules/braces': { dev: true } } };

test('the reviewed unfixed advisory is triaged only in development dependencies', () => {
  assert.deepEqual(blockedFindings(report(), dev), []);
  assert.deepEqual(blockedFindings(report(), { packages: {} }), ['braces']);
});
test('new high advisories and critical findings still fail', () => {
  assert.deepEqual(blockedFindings(report([{ url: 'https://github.com/advisories/new' }]), dev), ['braces']);
  const critical = report();
  critical.vulnerabilities.braces.severity = 'critical';
  assert.deepEqual(blockedFindings(critical, dev), ['braces']);
});
test('an unavailable database or unresolved dependency never passes', () => {
  assert.throws(() => blockedFindings({ error: { code: 'NETWORK' } }, dev));
  assert.throws(() => blockedFindings(report(['missing']), dev));
});
