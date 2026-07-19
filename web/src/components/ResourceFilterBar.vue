<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, shallowRef, useTemplateRef, watch } from 'vue';

interface FilterOption {
  label: string;
  value: string;
}

const props = withDefaults(defineProps<{
  modelValue: string;
  options: FilterOption[];
  previewLimit?: number;
  showPopular?: boolean;
}>(), {
  previewLimit: 6,
  showPopular: true,
});

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();

const optionContainer = useTemplateRef<HTMLElement>('optionContainer');
const expanded = shallowRef(false);
const overflowing = shallowRef(false);
let resizeObserver: ResizeObserver | undefined;

const visibleOptions = computed(() => {
  if (expanded.value) return props.options;
  const options = props.options.slice(0, props.previewLimit);
  if (
    props.modelValue !== 'all'
    && props.modelValue !== 'popular'
    && !options.some(option => option.value === props.modelValue)
  ) {
    const selected = props.options.find(option => option.value === props.modelValue);
    if (selected) return [selected, ...options.slice(0, Math.max(0, props.previewLimit - 1))];
  }
  return options;
});

const showToggle = computed(() =>
  expanded.value || props.options.length > props.previewLimit || overflowing.value
);

function select(value: string) {
  emit('update:modelValue', value);
}

function measureOverflow() {
  const element = optionContainer.value;
  overflowing.value = Boolean(element && !expanded.value && element.scrollWidth > element.clientWidth + 1);
}

function toggleExpanded() {
  expanded.value = !expanded.value;
  void nextTick(measureOverflow);
}

watch(
  () => [props.options, props.modelValue] as const,
  () => void nextTick(measureOverflow),
  { deep: true },
);

onMounted(() => {
  resizeObserver = new ResizeObserver(measureOverflow);
  if (optionContainer.value) resizeObserver.observe(optionContainer.value);
  measureOverflow();
});

onBeforeUnmount(() => resizeObserver?.disconnect());
</script>

<template>
  <div class="resource-filter-bar">
    <div
      ref="optionContainer"
      class="resource-filter-options"
      :class="{ 'is-expanded': expanded }"
    >
      <el-button
        size="small"
        :type="modelValue === 'all' ? 'primary' : undefined"
        @click="select('all')"
      >
        全部
      </el-button>
      <el-button
        v-if="showPopular"
        size="small"
        :type="modelValue === 'popular' ? 'primary' : undefined"
        @click="select('popular')"
      >
        常用
      </el-button>
      <el-button
        v-for="option in visibleOptions"
        :key="option.value"
        size="small"
        :type="modelValue === option.value ? 'primary' : undefined"
        :title="option.label"
        @click="select(option.value)"
      >
        {{ option.label }}
      </el-button>
    </div>
    <el-button
      v-if="showToggle"
      class="resource-filter-toggle"
      link
      size="small"
      :aria-expanded="expanded"
      @click="toggleExpanded"
    >
      {{ expanded ? '收起' : '更多' }}
    </el-button>
  </div>
</template>

<style scoped>
.resource-filter-bar {
  display: flex;
  flex: 1 1 280px;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.resource-filter-options {
  display: flex;
  flex: 1 1 auto;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}

.resource-filter-options.is-expanded {
  flex-wrap: wrap;
  overflow: visible;
  white-space: normal;
}

.resource-filter-options .el-button,
.resource-filter-toggle {
  flex: 0 0 auto;
  margin: 0;
}

.resource-filter-options .el-button {
  max-width: 140px;
  padding-inline: 9px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.resource-filter-toggle {
  padding-inline: 4px;
}

@media (max-width: 780px) {
  .resource-filter-bar {
    width: 100%;
  }
}
</style>
