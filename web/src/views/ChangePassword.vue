<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { changeInitialPassword, request, session } from '../api'

const router = useRouter()
const emit = defineEmits(['account-saved'])
const current = ref('')
const password = ref('')
const confirmation = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  error.value = ''
  if (password.value !== confirmation.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  busy.value = true
  try {
    if (session.value?.must_change_password) {
      await changeInitialPassword(password.value, confirmation.value)
      await router.replace({ path: '/login', query: { changed: '1' } })
    } else {
      await request('me/password', {
        current_password: current.value, new_password: password.value, re_password: confirmation.value
      }, { method: 'PUT' })
      current.value = password.value = confirmation.value = ''
      emit('account-saved', '密码已修改，下次请使用新密码登录。')
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
      <h1 class="form-title">{{ session?.must_change_password ? '修改初始密码' : '修改密码' }}</h1>
      <p v-if="session?.must_change_password">首次登录需要设置新密码。保存后，请使用新密码重新登录。</p>
      <template v-else>
        <label for="current-password">当前密码</label>
        <input id="current-password" v-model="current" type="password" autocomplete="current-password" required>
      </template>
      <label for="new-password">新密码</label>
      <input id="new-password" v-model="password" type="password" autocomplete="new-password" required>
      <small>6–128 个字符<span v-if="session?.role === 'admin'">，须与初始密码不同</span></small>
      <label for="confirm-password">确认新密码</label>
      <input id="confirm-password" v-model="confirmation" type="password" autocomplete="new-password" required>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button class="btn-info" :disabled="busy">{{ busy ? '保存中…' : '保存新密码' }}</button>
    </form>
  </main>
</template>
