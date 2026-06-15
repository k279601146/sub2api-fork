<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.totalProblems') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ total }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.totalOccurrences') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ totalOccurrences }}</p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.affectedDevices') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ totalAffectedDevices }}</p>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div class="border-b border-gray-100 p-5 dark:border-dark-700">
          <div class="flex flex-wrap items-end gap-3">
            <label class="min-w-[180px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.eventType') }}</span>
              <input v-model="filters.event_type" class="input mt-1 w-full" placeholder="backend_start_failed" />
            </label>
            <label class="min-w-[140px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.version') }}</span>
              <input v-model="filters.version" class="input mt-1 w-full" placeholder="1.0.0" />
            </label>
            <label class="min-w-[140px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.platform') }}</span>
              <input v-model="filters.platform" class="input mt-1 w-full" placeholder="win32" />
            </label>
            <label class="min-w-[140px] flex-1">
              <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ide.problems.status') }}</span>
              <select v-model="filters.status" class="input mt-1 w-full">
                <option value="">{{ t('common.all') }}</option>
                <option value="open">{{ t('admin.ide.problems.open') }}</option>
              </select>
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
                <th class="px-5 py-3">{{ t('admin.ide.problems.problem') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.problems.impact') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.problems.versionPlatform') }}</th>
                <th class="px-5 py-3">{{ t('admin.ide.problems.lastSeen') }}</th>
                <th class="px-5 py-3"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="problem in problems" :key="problem.problem_fingerprint">
                <td class="px-5 py-4 text-gray-900 dark:text-gray-100">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="rounded bg-red-50 px-2 py-1 text-xs font-semibold text-red-700 dark:bg-red-900/30 dark:text-red-300">
                      {{ problem.severity }}
                    </span>
                    <span class="font-mono text-xs">{{ problem.event_type }}</span>
                  </div>
                  <div class="mt-2 max-w-xl text-sm">{{ problem.summary || '-' }}</div>
                  <div class="mt-1 font-mono text-xs text-gray-500">{{ problem.problem_fingerprint }}</div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ problem.affected_installation_count }} {{ t('admin.ide.problems.devices') }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ problem.occurrence_count }} {{ t('admin.ide.problems.occurrences') }}</div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ problem.last_app_version || '-' }} / {{ problem.last_platform || '-' }}</div>
                  <div class="mt-2 flex max-w-sm flex-wrap gap-1">
                    <span v-for="item in distributionBadges(problem.version_distribution)" :key="`v-${item.name}`" class="rounded bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                      {{ item.name }} {{ item.count }}
                    </span>
                    <span v-for="item in distributionBadges(problem.platform_distribution)" :key="`p-${item.name}`" class="rounded bg-emerald-50 px-2 py-1 text-xs text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
                      {{ item.name }} {{ item.count }}
                    </span>
                  </div>
                </td>
                <td class="px-5 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ formatDate(problem.last_seen_at) }}</div>
                  <div class="mt-1 text-xs text-gray-500">{{ problem.status }}</div>
                </td>
                <td class="px-5 py-4 text-right">
                  <button class="btn btn-sm btn-secondary" :disabled="eventsLoading === problem.id" @click="selectProblem(problem)">
                    {{ t('admin.ide.problems.samples') }}
                  </button>
                </td>
              </tr>
              <tr v-if="!loading && problems.length === 0">
                <td colspan="5" class="px-5 py-8 text-center text-gray-500 dark:text-gray-400">
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

      <div v-if="selectedProblem" class="card overflow-hidden">
        <div class="flex items-center justify-between border-b border-gray-100 p-5 dark:border-dark-700">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.problems.recentSamples') }}</h2>
            <p class="mt-1 font-mono text-xs text-gray-500">{{ selectedProblem.problem_fingerprint }}</p>
          </div>
          <button class="btn btn-secondary" :disabled="eventsLoading === selectedProblem.id" @click="loadEvents(selectedProblem)">
            {{ t('common.refresh') }}
          </button>
        </div>
        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="event in events" :key="event.id" class="p-5">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="font-mono text-sm text-gray-900 dark:text-gray-100">{{ event.event_type }}</div>
              <div class="text-sm text-gray-500 dark:text-gray-400">{{ formatDate(event.occurred_at) }}</div>
            </div>
            <div class="mt-2 text-sm text-gray-700 dark:text-gray-300">{{ event.summary || '-' }}</div>
            <div class="mt-2 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span>{{ event.app_version || '-' }}</span>
              <span>{{ event.platform || '-' }} / {{ event.arch || '-' }}</span>
              <span class="font-mono">{{ event.installation_id || '-' }}</span>
            </div>
            <pre class="mt-3 overflow-x-auto rounded bg-gray-50 p-3 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ formatMetadata(event.metadata) }}</pre>
          </div>
          <div v-if="!eventsLoading && events.length === 0" class="p-8 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('common.noData') }}
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { IDEProblemEvent, IDEProblemInfo } from '@/api/admin/ide'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const limit = 50
const offset = ref(0)
const loading = ref(false)
const eventsLoading = ref<number | null>(null)
const error = ref('')
const problems = ref<IDEProblemInfo[]>([])
const events = ref<IDEProblemEvent[]>([])
const selectedProblem = ref<IDEProblemInfo | null>(null)
const total = ref(0)
const filters = reactive({
  event_type: '',
  version: '',
  platform: '',
  status: ''
})

const totalOccurrences = computed(() => problems.value.reduce((sum, item) => sum + item.occurrence_count, 0))
const totalAffectedDevices = computed(() => problems.value.reduce((sum, item) => sum + item.affected_installation_count, 0))

function formatDate(value?: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function distributionBadges(input?: Record<string, number>): Array<{ name: string; count: number }> {
  return Object.entries(input ?? {})
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 4)
}

function formatMetadata(metadata: Record<string, unknown>): string {
  return JSON.stringify(metadata ?? {}, null, 2)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await adminAPI.ide.listProblems({
      limit,
      offset: offset.value,
      event_type: filters.event_type || undefined,
      version: filters.version || undefined,
      platform: filters.platform || undefined,
      status: filters.status || undefined
    })
    problems.value = response.items
    total.value = response.total
    if (selectedProblem.value && !problems.value.some((item) => item.id === selectedProblem.value?.id)) {
      selectedProblem.value = null
      events.value = []
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function loadEvents(problem: IDEProblemInfo) {
  eventsLoading.value = problem.id
  error.value = ''
  try {
    const response = await adminAPI.ide.listProblemEvents(problem.id, { limit: 20 })
    events.value = response.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    eventsLoading.value = null
  }
}

async function selectProblem(problem: IDEProblemInfo) {
  selectedProblem.value = problem
  await loadEvents(problem)
}

function applyFilters() {
  offset.value = 0
  load()
}

function resetFilters() {
  filters.event_type = ''
  filters.version = ''
  filters.platform = ''
  filters.status = ''
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
