<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.sessions.authMode') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">JWT</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.sessions.revokeScope') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.sessions.allUserTokens') }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.sessions.client') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">myide-desktop</p>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="flex items-center justify-between border-b border-gray-100 p-5 dark:border-dark-700">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.sessions.activeList') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ total }} {{ t('common.total') }}</p>
          </div>
          <button class="btn btn-secondary" :disabled="loading" @click="load">
            {{ t('common.refresh') }}
          </button>
        </div>

        <div v-if="error" class="m-5 rounded-lg bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
          {{ error }}
        </div>

        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-5 py-3">{{ t('admin.ide.sessions.user') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.sessions.platform') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.sessions.version') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.sessions.lastUsed') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.sessions.status') }}</th>
                <th class="px-5 py-3"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="session in sessions" :key="session.id">
                <td class="px-5 py-4 text-gray-900 dark:text-gray-100">
                  <div>{{ session.user_email || `#${session.user_id}` }}</div>
                  <div class="mt-1 font-mono text-xs text-gray-500">{{ session.id }}</div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ session.platform || '-' }}</td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ session.client_version || '-' }}</td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ formatDate(session.last_used_at) }}</td>
                <td class="px-5 py-4">
                  <span
                    class="rounded px-2 py-1 text-xs font-semibold"
                    :class="session.revoked ? 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300' : 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300'"
                  >
                    {{ session.revoked ? t('admin.ide.sessions.revoked') : t('admin.ide.sessions.active') }}
                  </span>
                </td>
                <td class="px-5 py-4 text-right">
                  <button
                    class="btn btn-sm btn-danger"
                    :disabled="session.revoked || revoking === session.id"
                    @click="revoke(session.id)"
                  >
                    {{ t('admin.ide.sessions.revoke') }}
                  </button>
                </td>
              </tr>
              <tr v-if="!loading && sessions.length === 0">
                <td colspan="6" class="px-5 py-8 text-center text-gray-500 dark:text-gray-400">
                  {{ t('common.noData') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="border-b border-gray-100 p-5 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.sessions.endpoints') }}</h2>
        </div>
        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="endpoint in endpoints" :key="endpoint.path" class="grid gap-2 p-5 md:grid-cols-[120px_1fr_1fr] md:items-center">
            <span class="inline-flex w-fit rounded bg-primary-50 px-2 py-1 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ endpoint.method }}
            </span>
            <code class="text-sm text-gray-900 dark:text-gray-100">{{ endpoint.path }}</code>
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ endpoint.description }}</span>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { IDESessionInfo } from '@/api/admin/ide'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const loading = ref(false)
const revoking = ref('')
const error = ref('')
const sessions = ref<IDESessionInfo[]>([])
const total = ref(0)

const endpoints = [
  { method: 'POST', path: '/ide/auth/token', description: 'Web JWT to IDE JWT exchange' },
  { method: 'GET', path: '/ide/auth/me', description: 'Current IDE principal' },
  { method: 'POST', path: '/ide/auth/revoke', description: 'Revoke issued user JWTs' },
  { method: 'GET', path: '/ide/api/usage', description: 'IDE usage summary' },
  { method: 'GET', path: '/ide/api/plan', description: 'IDE subscription summary' },
]

function formatDate(value: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await adminAPI.ide.listSessions()
    sessions.value = response.items
    total.value = response.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function revoke(id: string) {
  revoking.value = id
  try {
    await adminAPI.ide.revokeSession(id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    revoking.value = ''
  }
}

onMounted(load)
</script>
