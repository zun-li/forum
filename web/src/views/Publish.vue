<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'

const router = useRouter()
const communities = ref([])
const communityID = ref('')
const title = ref('')
const content = ref('')
const error = ref('')
const busy = ref(false)

onMounted(async () => {
  try { communities.value = await request('communities') }
  catch (e) { error.value = e.message }
})
async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const post = await request('posts', { community_id: communityID.value, title: title.value, content: content.value })
    await router.push('/post/' + post.id)
  } catch (e) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="content publish-page">
    <form class="left" @submit.prevent="submit">
      <h1 class="post-name">我好想写点什么</h1>
      <select v-model="communityID" class="post-type-value" aria-label="选择一个频道" required>
        <option disabled value="">选择一个频道</option>
        <option v-for="community in communities" :key="community.id" :value="community.id">{{ community.name }}</option>
      </select>
      <div class="panel post-content">
        <h2><i class="iconfont icon-edit"></i> 发布帖子</h2>
        <input v-model="title" placeholder="标题" aria-label="标题" required>
        <div class="textarea-num">{{ Array.from(title).length }}/300</div>
        <textarea v-model="content" placeholder="内容" aria-label="内容" rows="10" required></textarea>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="actions">
          <RouterLink class="secondary" to="/">取消</RouterLink>
          <button class="primary" :disabled="busy">{{ busy ? '发表中…' : '发表' }}</button>
        </div>
      </div>
    </form>
    <aside class="right panel">
      <h2 class="r-c-title">发帖须知</h2>
      <p class="tip">选择与内容相关的频道，方便其他人找到你的帖子。</p>
      <p class="tip">标题最多 300 个字符，正文最多 8192 个字符。</p>
    </aside>
  </main>
</template>
