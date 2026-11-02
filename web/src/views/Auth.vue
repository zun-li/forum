<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { request, setSession } from '../api'

const props = defineProps({ signup: Boolean })
const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  if (busy.value) return
  error.value = ''
  if (props.signup && confirmation.value !== password.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  const body = { username: username.value.trim(), password: password.value }
  busy.value = true
  try {
    if (props.signup) {
      await request('signup', { ...body, re_password: confirmation.value })
      await router.push({ path: '/login', query: { registered: '1', next: route.query.next } })
    } else {
      await setSession(await request('login', body))
      const next = route.query.next
      await router.push(typeof next === 'string' && /^\/(?:publish|change-username|change-password|post\/\d+(?:#comments)?)?(?:\?.*)?$/.test(next) ? next : '/')
    }
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <form class="auth-card panel" @submit.prevent="submit">
      <h1 class="form-title">{{ signup ? '注册' : '登录' }}</h1>
      <p v-if="!signup && route.query.registered" role="status" class="notice">注册成功，请登录。</p>
      <p v-if="!signup && route.query.changed" role="status" class="notice">密码已修改，请使用新密码登录。</p>
      <label for="name">用户名</label>
      <input id="name" v-model="username" name="username" placeholder="用户名" autocomplete="username" required>
      <small v-if="signup">3–32 位英文字母、数字或下划线</small>
      <label for="pass">密码</label>
      <input id="pass" v-model="password" name="password" placeholder="密码" type="password" :autocomplete="signup ? 'new-password' : 'current-password'" required>
      <template v-if="signup">
        <small>6–128 个字符</small>
        <label for="re_pass">确认密码</label>
        <input id="re_pass" v-model="confirmation" name="re_password" type="password" placeholder="确认密码" autocomplete="new-password" required>
      </template>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button class="btn-info" :disabled="busy">{{ busy ? '提交中…' : '提交' }}</button>
    </form>
  </main>
</template>
