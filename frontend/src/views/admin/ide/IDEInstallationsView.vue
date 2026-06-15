<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-4">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.total') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.total_installations }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.activeDevices') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.active_devices }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.recentlyActive') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.recently_active }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.withErrors') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ stats.with_errors }}</p>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div class="card p-5">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.installations.versionDistribution') }}</h2>
          <div class="mt-4 space-y-3">
            <div v-for="item in stats.version_distribution" :key="item.name" class="space-y-1">
              <div class="flex items-center justify-between text-sm">
                <span class="font-mono text-gray-700 dark:text-gray-300">{{ item.name }}</span>
                <span class="text-gray-500 dark:text-gray-400">{{ item.count }}</span>
              </div>
              <div class="h-2 rounded bg-gray-100 dark:bg-dark-700">
                <div class="h-2 rounded bg-primary-500" :style="{ width: `${distributionPercent(item.count, stats.total_installations)}%` }"></div>
              </div>
            </div>
            <p v-if="stats.version_distribution.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.noData') }}</p>
          </div>
        </div>

        <div class="card p-5">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.installations.platformDistribution') }}</h2>
          <div class="mt-4 space-y-3">
            <div v-for="item in stats.platform_distribution" :key="item.name" class="space-y-1">
              <div class="flex items-center justify-between text-sm">
                <span class="font-mono text-gray-700 dark:text-gray-300">{{ item.name }}</span>
                <span class="text-gray-500 dark:text-gray-400">{{ item.count }}</span>
              </div>
              <div class="h-2 rounded bg-gray-100 dark:bg-dark-700">
                <div class="h-2 rounded bg-emerald-500" :style="{ width: `${distributionPercent(item.count, stats.total_installations)}%` }"></div>
              </div>
            </div>
            <p v-if="stats.platform_distribution.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.noData') }}</p>
          </div>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="border-b border-gray-100 p-5 dark:border-dark-700">
          <div class="flex flex-wrap items-end gap-3">
            <label class="min-w-[160px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.version') }}</span>
              <input v-model="filters.version" class="input mt-1 w-full" placeholder="1.0.0" />
            </label>
            <label class="min-w-[140px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.platform') }}</span>
              <input v-model="filters.platform" class="input mt-1 w-full" placeholder="win32" />
            </label>
            <label class="min-w-[140px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.installations.status') }}</span>
              <select v-model="filters.status" class="input mt-1 w-full">
                <option value="">{{ t('common.all') }}</option>
                <option value="active">{{ t('admin.ide.installations.active') }}</option>
              </select>
            </label>
            <label class="flex items-center gap-2 pb-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="filters.has_error" type="checkbox" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              {{ t('admin.ide.installations.onlyErrors') }}
            </label>
            <button class="btn btn-primary" :disabled="loading" @click="applyFilters">{{ t('common.search') }}</button>
            <button class="btn btn-secondary" :disabled="loading" @click="resetFilters">{{ t('common.reset') }}</button>
          </div>
        </div>

        <div v-if="error" class="m-5 rounded-lg bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
          {{ error }}
        </div>

        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-5 py-3">{{ t('admin.ide.installations.installation') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.installations.user') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.installations.platform') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.installations.version') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.installations.lastSeen') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.installations.status') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in installations" :key="item.installation_id">
                <td class="px-5 py-4 text-gray-900 dark:text-gray-100">
                  <div class="font-mono text-xs">{{ item.installation_id }}</div>
                  <div class="mt-1 font-mono text-xs text-gray-500">{{ item.device_id }}</div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ item.user_id ? `#${item.user_id}` : t('admin.ide.installations.anonymous') }}</td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">{{ platformLabel(item) }}</td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ item.app_version || '-' }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ item.engine_version || '-' }}</div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ formatDate(item.last_seen_at) }}</div>
                  <div v-if="item.last_error_at" class="mt-1 text-xs text-red-500">{{ t('admin.ide.installations.lastError') }} {{ formatDate(item.last_error_at) }}</div>
                </td>
                <td class="px-5 py-4">
                  <span class="rounded bg-green-50 px-2 py-1 text-xs font-semibold text-green-700 dark:bg-green-900/30 dark:text-green-300">
                    {{ item.status || '-' }}
                  </span>
                </td>
              </tr>
              <tr v-if="!loading && installations.length === 0">
                <td colspan="6" class="px-5 py-8 text-center text-gray-500 dark:text-gray-400">
                  {{ t('common.noData') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex items-center justify-between border-t border-gray-100 p-5 text-sm dark:border-dark-700">
          <span class="text-gray-500 dark:text-gray-400">{{ total }} {{ t('common.total') }}</span>
          <div class="flex gap-2">
            <button class="btn btn-secondary" :disabled="loading || offset === 0" @click="previousPage">{{ t('common.previous') }}</button>
            <button class="btn btn-secondary" :disabled="loading || offset + limit >= total" @click="nextPage">{{ t('common.next') }}</button>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { IDEInstallationInfo, IDEInstallationStats } from '@/api/admin/ide'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const limit = 50
const offset = ref(0)
const loading = ref(false)
const error = ref('')
const installations = ref<IDEInstallationInfo[]>([])
const total = ref(0)
const stats = ref<IDEInstallationStats>({
  total_installations: 0,
  active_devices: 0,
  recently_active: 0,
  with_errors: 0,
  version_distribution: [],
  platform_distribution: []
})
const filters = reactive({
  version: '',
  platform: '',
  status: '',
  has_error: false
})

function formatDate(value?: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function distributionPercent(count: number, totalCount: number): number {
  if (totalCount <= 0) return 0
  return Math.max(4, Math.round((count / totalCount) * 100))
}

function platformLabel(item: IDEInstallationInfo): string {
  return [item.platform, item.arch, item.channel].filter(Boolean).join(' / ') || '-'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await adminAPI.ide.listInstallations({
      limit,
      offset: offset.value,
      version: filters.version || undefined,
      platform: filters.platform || undefined,
      status: filters.status || undefined,
      has_error: filters.has_error || undefined
    })
    installations.value = response.items
    total.value = response.total
    stats.value = response.stats
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  offset.value = 0
  load()
}

function resetFilters() {
  filters.version = ''
  filters.platform = ''
  filters.status = ''
  filters.has_error = false
  offset.value = 0
  load()
}

function previousPage() {
  offset.value = Math.max(0, offset.value - limit)
  load()
}

function nextPage() {
  offset.value += limit
  load()
}

onMounted(load)
</script>
