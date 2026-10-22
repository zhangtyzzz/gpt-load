// Preserve the high-severity gate while triaging one unfixed build-only advisory.
import { readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const knownBuildOnly = new Set([
  'braces',
  'micromatch',
  'fast-glob',
  '@vue/eslint-config-typescript',
]);
const knownAdvisory = 'https://github.com/advisories/GHSA-vfj7-8cjw-p6xm';

export function blockedFindings(report, lock) {
  if (report.error || !report.vulnerabilities || !report.metadata?.vulnerabilities) {
    throw new Error('Incomplete npm audit response');
  }
  const entries = report.vulnerabilities;
  function advisoryURLs(name, seen = new Set()) {
    if (seen.has(name) || !entries[name]) throw new Error('Unresolved audit dependency');
    const next = new Set(seen).add(name);
    return entries[name].via.flatMap((entry) =>
      typeof entry === 'string' ? advisoryURLs(entry, next) : [entry.url],
    );
  }
  return Object.entries(entries)
    .filter(([, entry]) => ['high', 'critical'].includes(entry.severity))
    .filter(([name, entry]) => {
      const urls = advisoryURLs(name);
      // Reviewed 2026-10-22: braces has no published fix. Its glob patterns come
      // from trusted repository lint configuration, and it is absent at runtime.
      const triaged =
        knownBuildOnly.has(name) &&
        entry.severity === 'high' &&
        entry.nodes.length > 0 &&
        entry.nodes.every((node) => lock.packages?.[node]?.dev === true) &&
        urls.length > 0 &&
        urls.every((url) => url === knownAdvisory);
      return !triaged;
    })
    .map(([name]) => name);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const audit = spawnSync('npm', ['audit', '--json'], { cwd: 'web', encoding: 'utf8' });
  if (audit.error || ![0, 1].includes(audit.status)) throw new Error('npm audit failed');
  const report = JSON.parse(audit.stdout);
  const blocked = blockedFindings(report, JSON.parse(readFileSync('web/package-lock.json')));
  console.log(JSON.stringify(report.metadata.vulnerabilities));
  console.log(`Build-only advisory triage: ${knownAdvisory}; all new/runtime high findings block.`);
  if (blocked.length) throw new Error(`Untriaged high/critical dependencies: ${blocked.join(', ')}`);
}
