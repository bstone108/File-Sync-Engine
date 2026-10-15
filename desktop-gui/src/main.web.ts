import { mount } from 'svelte';
import App from './App.svelte';

const target = document.getElementById('app');
if (!target) {
  throw new Error('web GUI mount point is missing');
}

const app = mount(App, { target });

export default app;
