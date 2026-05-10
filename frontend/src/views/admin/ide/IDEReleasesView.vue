<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.ide.releases.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ide.releases.description') }}</p>
        </div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load">
            {{ t('common.refresh') }}
          </button>
          <button class="btn btn-primary" @click="showPublish = !showPublish">
            {{ t('admin.ide.releases.publish') }}
          </button>
        </div>
      </div>

      <div v-if="error" class="rounded-lg bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
        {{ error }}
      </div>

      <form v-if="showPublish" class="card grid gap-4 p-5 md:grid-cols-2" @submit.prevent="publish">
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.kind') }}</span>
          <select v-model="form.kind" class="input">
            <option value="app">app</option>
            <option value="engine">engine</option>
          </select>
        </label>
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.version') }}</span>
          <input v-model="form.version" class="input" placeholder="1.2.3" required />
        </label>
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.platform') }}</span>
          <select v-model="form.platform" class="input">
            <option value="win32-x64">win32-x64</option>
            <option value="darwin-arm64">darwin-arm64</option>
            <option value="darwin-x64">darwin-x64</option>
            <option value="linux-x64">linux-x64</option>
          </select>
        </label>
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.minApp') }}</span>
          <input v-model="form.min_app_version" class="input" placeholder="1.0.0" />
        </label>
        <label class="space-y-1 text-sm md:col-span-2">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.downloadUrl') }}</span>
          <input v-model="form.url" class="input" placeholder="https://cdn.example.com/engine.exe" required />
        </label>
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">SHA256</span>
          <input v-model="form.sha256" class="input font-mono" required />
        </label>
        <label class="space-y-1 text-sm">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.size') }}</span>
          <input v-model.number="form.size" class="input" type="number" min="0" />
        </label>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="form.is_mandatory" type="checkbox" class="rounded" />
          {{ t('admin.ide.releases.mandatory') }}
        </label>
        <label class="space-y-1 text-sm md:col-span-2">
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ide.releases.notes') }}</span>
          <textarea v-model="form.release_notes" class="input min-h-24"></textarea>
        </label>
        <div class="md:col-span-2">
          <button class="btn btn-primary" :disabled="publishing">
            {{ publishing ? t('common.loading') : t('admin.ide.releases.publish') }}
          </button>
        </div>
      </form>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div v-for="release in releases" :key="release.kind" class="card p-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-medium uppercase text-gray-500 dark:text-gray-400">{{ release.kind }}</p>
              <h2 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {{ release.latest_version || release.version || '-' }}
              </h2>
            </div>
            <span
              class="rounded px-2 py-1 text-xs font-semibold"
              :class="release.is_mandatory ? 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300' : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'"
            >
              {{ release.is_mandatory ? t('admin.ide.releases.mandatory') : t('admin.ide.releases.optional') }}
            </span>
          </div>

          <dl class="mt-5 space-y-3 text-sm">
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.ide.releases.current') }}</dt>
              <dd class="text-gray-900 dark:text-gray-100">{{ release.current_version || '-' }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.ide.releases.minApp') }}</dt>
              <dd class="text-gray-900 dark:text-gray-100">{{ release.min_app_version || '-' }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">SHA256</dt>
              <dd class="max-w-[14rem] truncate font-mono text-gray-900 dark:text-gray-100">{{ release.download?.sha256 || release.sha256 || '-' }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.ide.releases.size') }}</dt>
              <dd class="text-gray-900 dark:text-gray-100">{{ formatSize(release.download?.size) }}</dd>
            </div>
          </dl>

          <a
            v-if="release.download?.url || release.download_url"
            class="mt-5 block truncate text-sm font-medium text-primary-600 hover:underline dark:text-primary-400"
            :href="release.download?.url || release.download_url"
            target="_blank"
            rel="noreferrer"
          >
            {{ release.download?.url || release.download_url }}
          </a>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { IDEPublishReleaseRequest, IDEVersionInfo } from '@/api/admin/ide'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const loading = ref(false)
const publishing = ref(false)
const showPublish = ref(false)
const error = ref('')
const releases = ref<IDEVersionInfo[]>([])
const form = ref({
  kind: 'engine' as IDEPublishReleaseRequest['kind'],
  version: '',
  platform: 'win32-x64',
  min_app_version: '',
  url: '',
  sha256: '',
  size: 0,
  release_notes: '',
  is_mandatory: false
})

function formatSize(value: number | null | undefined): string {
  if (!value || value <= 0) return '-'
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await adminAPI.ide.listReleases()
    if (response.items.length > 0) {
      releases.value = response.items
    } else {
      releases.value = await Promise.all([
        adminAPI.ide.getVersion('app'),
        adminAPI.ide.getVersion('engine')
      ])
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function publish() {
  publishing.value = true
  error.value = ''
  try {
    await adminAPI.ide.publishRelease({
      kind: form.value.kind,
      version: form.value.version,
      min_app_version: form.value.min_app_version || undefined,
      release_notes: form.value.release_notes || undefined,
      is_mandatory: form.value.is_mandatory,
      binaries: {
        [form.value.platform]: {
          url: form.value.url,
          sha256: form.value.sha256,
          size: form.value.size || undefined
        }
      }
    })
    showPublish.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    publishing.value = false
  }
}

onMounted(load)
</script>
