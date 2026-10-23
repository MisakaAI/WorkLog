<script setup>
import { computed } from 'vue'
import {
  RiAddLine, RiCalendarEventLine, RiDeleteBinLine, RiFlagLine,
  RiLayoutColumnLine, RiListCheck, RiSearchLine, RiSunLine, RiMoonLine,
} from '@remixicon/vue'

const props = defineProps({ name: String, size: { type: [Number, String], default: 18 } })
const icons = {
  Add: RiAddLine, CalendarEvent: RiCalendarEventLine, Delete: RiDeleteBinLine,
  Flag: RiFlagLine, LayoutColumn: RiLayoutColumnLine, ListCheck: RiListCheck,
  Search: RiSearchLine, Sun: RiSunLine, Moon: RiMoonLine,
}
const icon = computed(() => icons[props.name])
</script>

<template><component :is="icon" :size="String(size)" aria-hidden="true" /></template>
