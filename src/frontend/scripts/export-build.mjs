import { rm, mkdir, cp, rename, access } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

function p(...parts) {
  return resolve(here, ...parts);
}

async function exists(path) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

async function exportTarget(target, destDir, destIndexName, sourceHtmlName = `${target}.html`) {
  const distDir = p('..', 'dist', target);
  const srcHtml = p('..', 'dist', target, sourceHtmlName);

  if (!(await exists(distDir))) {
    throw new Error(`Missing build output: ${distDir}`);
  }
  if (!(await exists(srcHtml))) {
    throw new Error(`Missing HTML entry: ${srcHtml}`);
  }

  // Clean destination
  await rm(destDir, { recursive: true, force: true });
  await mkdir(destDir, { recursive: true });

  // Copy everything
  await cp(distDir, destDir, { recursive: true });

  // Rename tab.html/widget.html -> index.html
  const destHtml = resolve(destDir, destIndexName);
  await rename(resolve(destDir, sourceHtmlName), destHtml);
}

await exportTarget('tab', p('..', '..', '..', 'web', 'ui'), 'index.html');
await exportTarget('widget-overview', p('..', '..', '..', 'web', 'widgets', 'overview'), 'index.html', 'widget.html');
await exportTarget('widget-configure', p('..', '..', '..', 'web', 'widgets', 'configure'), 'index.html', 'widget.html');
await exportTarget('setup', p('..', '..', '..', 'web', 'ui', 'setup'), 'index.html');
await rm(p('..', '..', '..', 'web', 'widgets', 'zone'), { recursive: true, force: true });

console.log('OK: exported tab -> web/ui, setup -> web/ui/setup, overview widget -> web/widgets/overview and configure widget -> web/widgets/configure');