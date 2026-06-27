import type { Directive } from 'vue';

import { usePermissionStore } from '@/stores/permission';

export const vPermission: Directive<HTMLElement, string> = {
  mounted(el, binding) {
    const store = usePermissionStore();
    if (!store.canDo(binding.value)) {
      el.style.display = 'none';
    }
  },
};
