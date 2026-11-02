<script setup>
import { ref } from 'vue'
import { changeUsername, session } from '../api'

const emit = defineEmits(['account-saved'])
const username = ref(session.value?.user_name || '')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await changeUsername(username.value)
    emit('account-saved', '名称已保存。')
  } catch (err) {
    error.value = err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <form class="auth-card panel" @submit.prevent="submit">
      <h1 class="form-title">修改名称</h1>
      <label for="new-username">新名称</label>
      <input id="new-username" v-model="username" autocomplete="username" required :disabled="busy" @input="error = ''">
      <small>3–32 位英文字母、数字或下划线。保存后，请使用此名称登录。</small>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button class="btn-info" :disabled="busy">{{ busy ? '保存中…' : '保存名称' }}</button>
    </form>
  </main>
</template>
