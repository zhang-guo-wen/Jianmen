<script setup lang="ts">
defineProps<{
  sessionId?: string;
}>();

const emit = defineEmits<{
  open: [sessionId: string];
}>();
</script>

<template>
  <el-link
    v-if="sessionId"
    class="session-id-link"
    type="primary"
    role="button"
    tabindex="0"
    @click.stop="emit('open', sessionId)"
    @keydown.enter.space.prevent.stop="emit('open', sessionId)"
  >
    {{ sessionId }}
  </el-link>
  <span v-else>-</span>
</template>

<style scoped>
.session-id-link.el-link:focus-visible {
  border-radius: 4px;
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
}
</style>
