<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'

const router = useRouter()
const name = ref('')
const introduction = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const channel = await request('admin/communities', { name: name.value, introduction: introduction.value })
    await router.push({ path: '/', query: { community_id: String(channel.id) } })
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
      <h1 class="form-title">新建频道</h1>
      <label for="channel-name">频道名称</label>
      <input id="channel-name" v-model="name" required>
      <small>最多 128 个字符</small>
      <label for="channel-intro">频道简介</label>
      <textarea id="channel-intro" v-model="introduction" rows="4" required></textarea>
      <small>最多 256 个字符</small>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="actions">
        <RouterLink class="secondary" to="/">取消</RouterLink>
        <button class="primary" :disabled="busy">{{ busy ? '创建中…' : '创建频道' }}</button>
      </div>
    </form>
  </main>
</template>
