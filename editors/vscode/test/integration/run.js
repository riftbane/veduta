'use strict';
// Starts a real VS Code with the extension on a game project and runs suite.js inside it.
// CI: xvfb-run -a node test/integration/run.js <project>, with veduta on PATH.
const path = require('path');
const { runTests } = require('@vscode/test-electron');

async function main() {
  const project = process.argv[2];
  if (!project) {
    throw new Error('usage: node test/integration/run.js <game project>');
  }
  await runTests({
    extensionDevelopmentPath: path.resolve(__dirname, '..', '..'),
    extensionTestsPath: path.resolve(__dirname, 'suite.js'),
    launchArgs: [path.resolve(project), '--disable-extensions', '--disable-workspace-trust'],
  });
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
