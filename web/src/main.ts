import ElementPlus from 'element-plus';
import 'element-plus/dist/index.css';

import { createApp } from 'vue';

import App from './App.vue';
import i18n from './i18n';
import router from './router';
import './styles/main.css';

createApp(App).use(i18n).use(router).use(ElementPlus).mount('#app');
