<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { request, session } from '../api'

const props = defineProps({ postId: { type: String, required: true } })
const route = useRoute()
const path = 'posts/' + props.postId + '/comments'
const section = ref(null)
const items = ref([])
const before = ref('')
const draft = ref('')
const length = computed(() => Array.from(draft.value.trim()).length)
const loading = ref(false)
const saving = ref(false)
const busy = computed(() => loading.value || saving.value)
const loadError = ref('')
const actionError = ref('')
const notice = ref('')

async function load(cursor = '') {
  loading.value = true
  loadError.value = ''
  try {
    const page = await request(path + (cursor ? '?before=' + cursor : ''))
    items.value = cursor ? [...page.items, ...items.value] : page.items
    before.value = page.has_more ? page.items[0].id : ''
  } catch (e) {
    loadError.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (route.hash === '#comments') section.value.scrollIntoView()
  load()
})

async function publish() {
  if (busy.value || !length.value || length.value > 1000) return
  saving.value = true
  actionError.value = notice.value = ''
  try {
    await request(path, { content: draft.value.trim() })
    notice.value = '留言已发表'
    draft.value = ''
    before.value = ''
    await load()
  } catch (e) {
    actionError.value = e.message
  } finally {
    saving.value = false
  }
}

async function remove(comment) {
  if (busy.value || !window.confirm('确定删除这条留言吗？删除后无法恢复。')) return
  saving.value = true
  actionError.value = notice.value = ''
  try {
    const prefix = session.value?.role === 'admin' ? 'admin/' : ''
    await request(prefix + path + '/' + comment.id, undefined, { method: 'DELETE' })
    items.value = items.value.filter(item => item.id !== comment.id)
    notice.value = '留言已删除'
  } catch (e) {
    actionError.value = e.message
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section id="comments" ref="section" class="panel comments" aria-labelledby="comments-title">
    <h2 id="comments-title">留言</h2>
    <form v-if="session" @submit.prevent="publish">
      <label for="comment-text">写留言</label>
      <textarea id="comment-text" v-model="draft" rows="3" required :disabled="saving"
        aria-describedby="comment-limit" :aria-invalid="length > 1000" />
      <div class="actions">
        <small id="comment-limit">{{ length }} / 1000 字</small>
        <button class="primary" :disabled="busy || !length || length > 1000">发表留言</button>
      </div>
    </form>
    <RouterLink v-else class="comment-link" :to="{ path: '/login', query: { next: route.path + '#comments' } }">登录后写留言</RouterLink>
    <p v-if="actionError" class="error" role="alert">{{ actionError }}</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <p v-if="loadError" class="error" role="alert">
      {{ loadError }} <button :disabled="busy" @click="load(before)">重新加载留言</button>
    </p>
    <button v-else-if="before" class="comment-link" :disabled="busy" @click="load(before)">查看更早的留言</button>
    <p v-if="loading" class="status" role="status">正在加载留言…</p>
    <p v-else-if="!items.length && !before && !loadError" class="status">还没有留言。</p>
    <ul aria-label="留言列表">
      <li v-for="comment in items" :key="comment.id">
        <div class="meta">{{ comment.author_name }} · <time :datetime="comment.create_time">{{ new Date(comment.create_time).toLocaleString('zh-CN') }}</time></div>
        <p>{{ comment.content }}</p>
        <button v-if="session?.user_id === comment.author_id || session?.role === 'admin'"
          class="delete-link" :disabled="busy" @click="remove(comment)">删除留言</button>
      </li>
    </ul>
  </section>
</template>
