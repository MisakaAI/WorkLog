<script setup>
import { nextTick, ref, useId } from 'vue'
import { ElDatePicker } from 'element-plus'

defineOptions({ inheritAttrs: false })
const model = defineModel()
// 每个实例使用独立类名，避免多个日期弹层把自定义页脚挂到彼此内部。
const popperClass = `task-date-picker-${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`
const footerTarget = ref(null)
const defaultDate = ref(new Date())

async function onVisibleChange(visible) {
  footerTarget.value = null
  if (!visible) return
  await nextTick()
  footerTarget.value = document.querySelector(`.${popperClass} .el-picker-panel__body-wrapper`)
}

function returnToNow() {
  // 修改面板默认日期只切回当前月份，不会替用户选中或覆盖任务日期。
  defaultDate.value = new Date()
}
</script>

<template>
  <ElDatePicker
    v-model="model"
    v-bind="$attrs"
    :popper-class="`task-date-picker ${popperClass}`"
    :default-value="defaultDate"
    @visible-change="onVisibleChange"
  />
  <Teleport v-if="footerTarget" :to="footerTarget">
    <div class="task-date-picker-footer">
      <button type="button" class="task-date-picker-now" aria-label="返回当前月份" @click.stop="returnToNow">现在</button>
    </div>
  </Teleport>
</template>

<style>
.task-date-picker .el-date-table td.today:not(.current) .el-date-table-cell__text {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
  border-radius: 50%;
}
.task-date-picker-footer {
  clear: both;
  display: flex;
  justify-content: flex-end;
  padding: 8px 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.task-date-picker-now {
  padding: 4px 8px;
  border: 0;
  background: transparent;
  color: var(--el-color-primary);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.task-date-picker-now:hover { color: var(--el-color-primary-light-3); }
.task-date-picker-now:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
</style>
