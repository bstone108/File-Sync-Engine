import { mount } from 'svelte';
import App from './App.svelte';
import { installWailsNativeShellBridge } from './lib/wailsNativeShell';

installWailsNativeShellBridge();

const app = mount(App, {
  target: document.getElementById('app')!
});

export default app;
