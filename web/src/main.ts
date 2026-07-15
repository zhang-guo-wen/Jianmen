import { createPinia } from 'pinia';
import {
  ElAlert,
  ElAside,
  ElButton,
  ElCard,
  ElCheckbox,
  ElCheckboxGroup,
  ElCollapse,
  ElCollapseItem,
  ElConfigProvider,
  ElContainer,
  ElDatePicker,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElDivider,
  ElDrawer,
  ElDropdown,
  ElDropdownItem,
  ElDropdownMenu,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElHeader,
  ElIcon,
  ElInput,
  ElInputNumber,
  ElLoading,
  ElMain,
  ElMenu,
  ElMenuItem,
  ElOption,
  ElOptionGroup,
  ElPagination,
  ElRadio,
  ElRadioButton,
  ElRadioGroup,
  ElSegmented,
  ElSelect,
  ElSlider,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTabPane,
  ElTabs,
  ElTag,
  ElTooltip,
  ElTree,
} from 'element-plus';
import 'element-plus/dist/index.css';
import 'element-plus/theme-chalk/dark/css-vars.css';

import { createApp } from 'vue';

import App from './App.vue';
import { getToken } from './api/client';
import { usePreferencesStore } from './stores/preferences';
import { vPermission } from './directives/permission';
import i18n from './i18n';
import router from './router';
import './styles/main.css';

const app = createApp(App);
const elementComponents = [
  ElAlert,
  ElAside,
  ElButton,
  ElCard,
  ElCheckbox,
  ElCheckboxGroup,
  ElCollapse,
  ElCollapseItem,
  ElConfigProvider,
  ElContainer,
  ElDatePicker,
  ElDescriptions,
  ElDescriptionsItem,
  ElDialog,
  ElDivider,
  ElDrawer,
  ElDropdown,
  ElDropdownItem,
  ElDropdownMenu,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElHeader,
  ElIcon,
  ElInput,
  ElInputNumber,
  ElMain,
  ElMenu,
  ElMenuItem,
  ElOption,
  ElOptionGroup,
  ElPagination,
  ElRadio,
  ElRadioButton,
  ElRadioGroup,
  ElSegmented,
  ElSelect,
  ElSlider,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTabPane,
  ElTabs,
  ElTag,
  ElTooltip,
  ElTree,
];

app.use(i18n);
app.use(router);
const pinia = createPinia();
app.use(pinia);
for (const component of elementComponents) {
  app.use(component);
}
app.use(ElLoading);
app.directive('permission', vPermission);

const preferences = usePreferencesStore(pinia);
preferences.apply();

async function mountApp() {
  const publicPaths = new Set(['/login', '/setup']);
  if (getToken() && !publicPaths.has(window.location.pathname)) {
    await preferences.fetch().catch(() => undefined);
  }
  app.mount('#app');
}

void mountApp();
