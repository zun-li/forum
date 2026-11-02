<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { request, session } from '../api'

const props = defineProps({ post: { type: Object, required: true } })
const emit = defineEmits(['changed', 'error'])
const router = useRouter()
const route = useRoute()
const busy = ref(false)

async function vote(direction) {
  if (busy.value) return
  if (!session.value) {
    await router.push({ path: '/login', query: { next: route.fullPath } })
    return
  }
  busy.value = true
  emit('error', '')
  try {
    await request('votes', { post_id: props.post.id, direction })
    emit('changed')
  } catch (e) {
    emit('error', e.message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="post vote-column">
    <button class="vote iconfont icon-up" aria-label="赞成" title="赞成" :disabled="busy" @click="vote(1)"></button>
    <span class="text" aria-label="赞成票数">{{ post.vote_num }}</span>
    <button class="vote iconfont icon-down" aria-label="反对" title="反对" :disabled="busy" @click="vote(-1)"></button>
    <button class="cancel-vote" title="取消投票" :disabled="busy" @click="vote(0)">撤票</button>
  </div>
</template>
