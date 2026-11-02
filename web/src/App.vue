<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { session, sessionNotice, logout } from './api'

const router = useRouter()
const route = useRoute()
const menu = ref(null)
const trigger = ref(null)
const menuOpen = ref(false)
const accountQuery = computed(() => ({ from: route.meta.account ? route.query.from : route.fullPath }))
const accountNotice = ref('')
const removeNoticeHook = router.afterEach((to, from) => {
  if (!from.meta.account || to.meta.account) accountNotice.value = ''
})
onUnmounted(removeNoticeHook)

function accountSaved(message) {
  const from = route.query.from
  const origin = typeof from === 'string' && /^\/(?![\/\\])/.test(from) ? router.resolve(from) : null
  const destination = origin?.matched.length && !origin.meta.account ? origin.fullPath : '/'
  accountNotice.value = message
  if (window.history.state?.back === destination) router.back()
  else router.replace(destination)
}

function closeOutside(event) {
  if (!menu.value?.contains(event.target)) menuOpen.value = false
}
function closeOnEscape() {
  if (!menuOpen.value) return
  menuOpen.value = false
  trigger.value?.focus()
}
onMounted(() => document.addEventListener('pointerdown', closeOutside))
onUnmounted(() => document.removeEventListener('pointerdown', closeOutside))
const loggingOut = ref(false)
const logoutError = ref('')
async function signOut() {
  loggingOut.value = true
  logoutError.value = ''
  try {
    await logout()
    await router.push('/')
  } catch (err) {
    logoutError.value = err.message
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <header class="header">
    <RouterLink class="logo" to="/">Forum 论坛</RouterLink>
    <nav v-if="session" aria-label="用户">
      <div ref="menu" class="account-menu" @keydown.esc="closeOnEscape"
        @focusout="menuOpen = menuOpen && menu.contains($event.relatedTarget)">
        <button ref="trigger" class="user" aria-controls="account-options" :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen">
          <span>{{ session.user_name }}</span><span aria-hidden="true">▾</span>
        </button>
        <div v-show="menuOpen" id="account-options" class="account-options panel">
          <RouterLink v-if="session.role !== 'admin' && !session.must_change_password"
            :to="{ path: '/change-username', query: accountQuery }" :replace="!!$route.meta.account" @click="menuOpen = false">修改名称</RouterLink>
          <RouterLink :to="{ path: '/change-password', query: accountQuery }"
            :replace="!!$route.meta.account" @click="menuOpen = false">修改密码</RouterLink>
        </div>
      </div>
      <button class="login-btn" :disabled="loggingOut" @click="signOut">登出</button>
    </nav>
    <nav v-else aria-label="账户">
      <RouterLink class="login-btn" to="/login">登录</RouterLink>
      <RouterLink class="login-btn" to="/signup">注册</RouterLink>
    </nav>
  </header>
  <p v-if="sessionNotice" class="notice" role="status">{{ sessionNotice }}</p>
  <p v-if="accountNotice && !$route.meta.account" class="notice" role="status">{{ accountNotice }}</p>
  <p v-if="logoutError" class="error" role="alert">{{ logoutError }}</p>
  <RouterView :key="$route.meta.account ? $route.path + ':' + session?.user_id : $route.path" @account-saved="accountSaved" />
</template>
