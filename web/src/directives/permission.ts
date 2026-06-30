import { watchEffect, type Directive, type DirectiveBinding, type WatchStopHandle } from 'vue';

import { usePermissionStore } from '@/stores/permission';

const stopKey = Symbol('permissionDirectiveStop');

type PermissionElement = HTMLElement & {
  [stopKey]?: WatchStopHandle;
};

function stopPermissionWatch(el: PermissionElement) {
  el[stopKey]?.();
  delete el[stopKey];
}

function startPermissionWatch(el: PermissionElement, action: string) {
  stopPermissionWatch(el);
  const store = usePermissionStore();
  el[stopKey] = watchEffect(() => {
    el.style.display = store.canDo(action) ? '' : 'none';
  });
}

export const vPermission: Directive<PermissionElement, string> = {
  mounted(el, binding: DirectiveBinding<string>) {
    startPermissionWatch(el, binding.value);
  },
  updated(el, binding: DirectiveBinding<string>) {
    if (binding.value !== binding.oldValue) {
      startPermissionWatch(el, binding.value);
    }
  },
  unmounted(el) {
    stopPermissionWatch(el);
  },
};
