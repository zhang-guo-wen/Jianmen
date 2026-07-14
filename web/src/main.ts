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
} from 'element-plus';
import 'element-plus/dist/index.css';

import { createApp } from 'vue';

import App from './App.vue';
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
];

app.use(i18n);
app.use(router);
app.use(createPinia());
for (const component of elementComponents) {
  app.use(component);
}
app.use(ElLoading);
app.directive('permission', vPermission);
app.mount('#app');
