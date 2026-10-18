import { BoobaTerminal, installActionBar, installRendererHud, parseRendererFromURL } from './booba/booba.js';

const demos = {
  mermaid: 'Mermaid diagrams',
  gallery: 'Sample gallery',
  stage: 'Vector stage',
  charts: 'Stage charts',
  svgview: 'SVG viewer',
};
const help = {
  mermaid: 'n/p: next/prev diagram · c: theme · m: raster/info · f: fit · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
  gallery: 'n/p: next/prev sample · m: raster/info · f: fit mode · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
  stage: 's: next scene · c: palette · ↑/↓: detail · m: raster/info · g: glyph/kitty · r: reset · q: quit.',
  charts: 't: bar/line · d: next dataset · v: vary values · m: raster/info · f: fit mode · g: glyph/kitty · r: reset · q: quit.',
  svgview: 'm: raster/info · c: generate demo chart · f: fit mode · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
};
const requested = new URLSearchParams(location.search).get('demo');
const demo = Object.hasOwn(demos, requested) ? requested : 'gallery';
const status = document.querySelector('#status');
document.querySelector('#title').textContent = demos[demo];
document.title = `${demos[demo]} · ntsvg`;
const restart = new URL(location.href);
restart.searchParams.set('demo', demo);
document.querySelector('#restart').href = restart.href;
for (const link of document.querySelectorAll('[data-demo]')) {
  if (link.dataset.demo === demo) link.setAttribute('aria-current', 'page');
}
document.querySelector('#help').textContent = help[demo];
function failed(error) {
  status.dataset.error = '';
  status.textContent = `Could not run the demo: ${error.message}. Try Restart.`;
  console.error(error);
}
async function main() {
  const go = new Go();
  const response = await fetch(`demos/${demo}.wasm`);
  if (!response.ok) throw new Error(`Download failed (${response.status})`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  const running = go.run(instance);
  running.then(() => { status.textContent = 'Session ended. Choose Restart to run again.'; }, failed);
  const terminal = new BoobaTerminal('terminal', { renderer: parseRendererFromURL(), allowOSC7501Actions: true });
  await terminal.init();
  terminal.setActionKeyEncoder({
    canEncode: gesture => terminal.encodeKey(gesture) !== null,
    encode: gesture => terminal.encodeKey(gesture),
  });
  installActionBar(terminal.actions, {
    parent: document.querySelector('#controls'),
    openKeyboard: () => terminal.focus(),
    onLayout: () => terminal.fitAddon?.fit(),
  });
  terminal.onActionError = failed;
  window.addEventListener('pagehide', () => terminal.dispose(), { once: true });
  terminal.connectWasm();
  terminal.focus();
  installRendererHud(terminal.term, {
    parent: document.querySelector('.terminal-frame'),
    position: 'absolute',
    bindToggleHotkey: false,
  });
  status.textContent = 'Running locally in your browser. No terminal server required.';
}
main().catch(failed);
