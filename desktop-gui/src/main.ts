import App from './App.svelte';
import { installWailsNativeShellBridge } from './lib/wailsNativeShell';

installWailsNativeShellBridge();

const app = new App({
  target: document.getElementById('app') as HTMLElement,
});

export default app;
