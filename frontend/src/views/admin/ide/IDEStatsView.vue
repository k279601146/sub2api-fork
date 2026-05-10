<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.telemetry') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">/ide/api/telemetry</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.usage') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">/ide/api/usage</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.plan') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">/ide/api/plan</p>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.activeSessions') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.active_sessions }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.revokedSessions') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.revoked_sessions }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.totalSessions') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.total_sessions }}</p>
        </div>
      </div>

      <div class="card p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.stats.healthCheck') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.stats.healthCheckDesc') }}</p>
          </div>
          <button class="btn btn-primary" :disabled="sending" @click="sendProbe">
            {{ sending ? t('common.loading') : t('admin.ide.stats.sendProbe') }}
          </button>
        </div>

        <p
          v-if="message"
          class="mt-4 rounded-lg p-3 text-sm"
          :class="success ? 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'"
        >
          {{ message }}
        </p>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { IDEStatsResponse } from '@/api/admin/ide'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const sending = ref(false)
const success = ref(false)
const message = ref('')
const stats = ref<IDEStatsResponse>({
  active_sessions: 0,
  revoked_sessions: 0,
  total_sessions: 0
})

async function sendProbe() {
  sending.value = true
  message.value = ''
  try {
    await adminAPI.ide.reportTelemetry([
      {
        type: 'admin_probe',
        timestamp: new Date().toISOString(),
        data: { source: 'admin_console' }
      }
    ])
    success.value = true
    message.value = t('admin.ide.stats.probeOk')
  } catch (err) {
    success.value = false
    message.value = err instanceof Error ? err.message : String(err)
  } finally {
    sending.value = false
  }
}

async function loadStats() {
  try {
    stats.value = await adminAPI.ide.getStats()
  } catch {
    stats.value = {
      active_sessions: 0,
      revoked_sessions: 0,
      total_sessions: 0
    }
  }
}

onMounted(loadStats)
</script>
