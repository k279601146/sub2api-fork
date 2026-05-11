<template>
  <main class="ide-auth-page">
    <section class="ide-auth-panel">
      <div class="ide-auth-icon" aria-hidden="true">
        <div class="ide-auth-mark"></div>
      </div>

      <template v-if="status === 'loading'">
        <h1>正在准备授权</h1>
        <p class="ide-auth-muted">请稍候，正在连接 IDE 客户端。</p>
      </template>

      <template v-else-if="status === 'error'">
        <h1>无法完成授权</h1>
        <p class="ide-auth-copy">{{ errorMessage }}</p>
        <button class="ide-auth-button ide-auth-button-primary" type="button" @click="goToLogin">
          重新登录
        </button>
      </template>

      <template v-else-if="status === 'approved'">
        <h1>Signed in to Codex</h1>
        <p class="ide-auth-muted">You may now close this page</p>
      </template>

      <template v-else>
        <h1>使用 ChatGPT 登录到 Codex</h1>

        <div class="ide-auth-account">
          <span class="ide-auth-account-dot" aria-hidden="true"></span>
          <span>{{ accountLabel }}</span>
        </div>

        <div class="ide-auth-copy">
          <p>继续操作后，ChatGPT 将向 Codex 提供你的姓名、电子邮件地址和个人资料头像以关联你的帐户。</p>
          <p>Codex 不会收到你的聊天历史记录。</p>
          <p>在你使用 Codex 时：</p>
          <p>
            该功能由你的 ChatGPT 帐户提供支持，并使用你当前套餐的速率限制、训练及语言偏好设置。
            ChatGPT 使用条款和隐私政策适用于与 Codex 共享的数据。
          </p>
          <p>Codex 可能存在错误。请务必审查其编写的代码和执行的命令。</p>
        </div>

        <div class="ide-auth-actions">
          <button
            class="ide-auth-button ide-auth-button-secondary"
            :disabled="isSubmitting"
            type="button"
            @click="cancelAuthorization"
          >
            取消
          </button>
          <button
            class="ide-auth-button ide-auth-button-primary"
            :disabled="isSubmitting"
            type="button"
            @click="approveAuthorization"
          >
            {{ isSubmitting ? '正在返回 IDE' : '继续' }}
          </button>
        </div>

        <nav class="ide-auth-links" aria-label="Legal links">
          <router-link to="/legal/terms">使用条款</router-link>
          <span aria-hidden="true">|</span>
          <router-link to="/legal/privacy">隐私政策</router-link>
        </nav>
      </template>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores'
import {
  approveIDELogin,
  authorizeIDELogin,
  buildIDECancelRedirect
} from '@/api/ideAuth'
import type { IDEAuthorizeResponse } from '@/api/ideAuth'

type PageStatus = 'loading' | 'ready' | 'approved' | 'error'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const status = ref<PageStatus>('loading')
const errorMessage = ref('')
const authorization = ref<IDEAuthorizeResponse | null>(null)
const isSubmitting = ref(false)

const accountLabel = computed(() => {
  return authStore.user?.email || authStore.user?.username || '已登录账户'
})

onMounted(async () => {
  authStore.checkAuth()
  if (!authStore.isAuthenticated) {
    await goToLogin()
    return
  }

  await prepareAuthorization()
})

async function prepareAuthorization(): Promise<void> {
  status.value = 'loading'
  errorMessage.value = ''

  const codeChallenge = readQueryString('code_challenge')
  const redirectURI = readQueryString('redirect_uri')
  const clientID = readQueryString('client_id')
  const codeChallengeMethod = readQueryString('code_challenge_method') || 'S256'

  if (!codeChallenge || !redirectURI) {
    errorMessage.value = '授权链接缺少 code_challenge 或 redirect_uri。'
    status.value = 'error'
    return
  }

  try {
    const params = {
      code_challenge: codeChallenge,
      code_challenge_method: codeChallengeMethod,
      redirect_uri: redirectURI
    } as {
      code_challenge: string
      code_challenge_method: string
      redirect_uri: string
      client_id?: string
    }
    if (clientID) {
      params.client_id = clientID
    }
    authorization.value = await authorizeIDELogin(params)
    status.value = 'ready'
  } catch (error) {
    errorMessage.value = errorMessageFromUnknown(error)
    status.value = 'error'
  }
}

async function approveAuthorization(): Promise<void> {
  const state = authorization.value?.state
  if (!state || isSubmitting.value) {
    return
  }

  isSubmitting.value = true
  errorMessage.value = ''

  try {
    const approved = await approveIDELogin(state)
    status.value = 'approved'
    window.location.assign(approved.redirect_url)
  } catch (error) {
    errorMessage.value = errorMessageFromUnknown(error)
    status.value = 'error'
    isSubmitting.value = false
  }
}

function cancelAuthorization(): void {
  const redirectURI = authorization.value?.redirect_uri || readQueryString('redirect_uri')
  if (!redirectURI) {
    void router.replace('/dashboard')
    return
  }

  try {
    window.location.assign(buildIDECancelRedirect(redirectURI))
  } catch {
    void router.replace('/dashboard')
  }
}

async function goToLogin(): Promise<void> {
  await router.replace({
    path: '/login',
    query: {
      redirect: route.fullPath
    }
  })
}

function readQueryString(key: string): string {
  const value = route.query[key]
  if (Array.isArray(value)) {
    return typeof value[0] === 'string' ? value[0] : ''
  }
  return typeof value === 'string' ? value : ''
}

function errorMessageFromUnknown(error: unknown): string {
  if (typeof error === 'string' && error.trim()) {
    return error.trim()
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  const record = error as { message?: unknown }
  if (typeof record?.message === 'string' && record.message.trim()) {
    return record.message
  }
  return '授权失败，请返回 IDE 后重试。'
}
</script>

<style scoped>
.ide-auth-page {
  min-height: 100vh;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  background: #fff;
  color: #050505;
  padding: 12vh 24px 48px;
}

.ide-auth-panel {
  width: min(100%, 470px);
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.ide-auth-icon {
  width: 64px;
  height: 64px;
  display: grid;
  place-items: center;
  border: 1px solid #e7e7e7;
  border-radius: 14px;
  box-shadow: 0 14px 34px rgb(0 0 0 / 8%);
}

.ide-auth-mark {
  width: 30px;
  height: 30px;
  border: 3px solid currentColor;
  border-radius: 999px;
  position: relative;
}

.ide-auth-mark::before {
  content: '';
  position: absolute;
  left: 7px;
  top: 9px;
  width: 5px;
  height: 5px;
  border-left: 2px solid currentColor;
  border-bottom: 2px solid currentColor;
}

.ide-auth-mark::after {
  content: '';
  position: absolute;
  right: 7px;
  top: 14px;
  width: 8px;
  height: 2px;
  background: currentColor;
  border-radius: 999px;
}

h1 {
  margin: 24px 0 0;
  font-size: 31px;
  line-height: 1.25;
  font-weight: 600;
  letter-spacing: 0;
}

.ide-auth-account {
  margin-top: 34px;
  max-width: 100%;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid #dedede;
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 13px;
  line-height: 1;
}

.ide-auth-account-dot {
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-radius: 999px;
}

.ide-auth-copy {
  margin-top: 32px;
  width: 100%;
  text-align: left;
  color: #303744;
  font-size: 15px;
  line-height: 1.55;
}

.ide-auth-copy p + p {
  margin-top: 14px;
}

.ide-auth-muted {
  margin-top: 18px;
  color: #565656;
  font-size: 15px;
}

.ide-auth-actions {
  margin-top: 34px;
  width: 100%;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.ide-auth-button {
  height: 52px;
  border-radius: 999px;
  border: 1px solid #d9d9d9;
  font-size: 16px;
  font-weight: 600;
  transition: background-color 0.16s ease, border-color 0.16s ease, opacity 0.16s ease;
}

.ide-auth-button:disabled {
  cursor: default;
  opacity: 0.62;
}

.ide-auth-button-primary {
  background: #111;
  border-color: #111;
  color: #fff;
}

.ide-auth-button-primary:not(:disabled):hover {
  background: #252525;
}

.ide-auth-button-secondary {
  background: #fff;
  color: #111;
}

.ide-auth-button-secondary:not(:disabled):hover {
  background: #f7f7f7;
}

.ide-auth-links {
  margin-top: 50px;
  display: inline-flex;
  gap: 12px;
  color: #4f4f4f;
  font-size: 14px;
}

.ide-auth-links a {
  text-decoration: underline;
  text-underline-offset: 3px;
}

@media (max-width: 560px) {
  .ide-auth-page {
    padding-top: 8vh;
  }

  h1 {
    font-size: 26px;
  }

  .ide-auth-actions {
    grid-template-columns: 1fr;
  }
}
</style>
