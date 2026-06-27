import { createPinia } from 'pinia';
import ElementPlus from 'element-plus';
import 'element-plus/dist/index.css';

import { createApp } from 'vue';

import App from './App.vue';
import { vPermission } from './directives/permission';
import i18n from './i18n';
import router from './router';
import './styles/main.css';

const app = createApp(App);
app.use(i18n);
app.use(router);
app.use(createPinia());
app.use(ElementPlus);
app.directive('permission', vPermission);
app.mount('#app');
