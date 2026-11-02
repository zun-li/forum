<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import VoteButtons from '../components/VoteButtons.vue'
import PostComments from '../components/PostComments.vue'
import { request, session } from '../api'

const props = defineProps({ id: { type: String, required: true } })
const router = useRouter()
const postPath = 'posts/' + encodeURIComponent(props.id)
const post = ref(null)
const error = ref('')
const confirmDialog = ref(null)
const deleting = ref(false)

async function load() {
  try {
    post.value = await request(postPath)
    error.value = ''
  } catch (e) {
    error.value = e.message
  }
}
onMounted(load)

async function deletePost() {
  if (deleting.value) return
  deleting.value = true
  error.value = ''
  try {
    const path = session.value?.role === 'admin' ? 'admin/' + postPath : postPath
    await request(path, undefined, { method: 'DELETE' })
    if (router.currentRoute.value.params.id === props.id) {
      await router.replace({ path: '/', state: { postDeleted: true } })
    }
  } catch (e) {
    confirmDialog.value?.close()
    error.value = e.message
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <main class="content detail-page">
    <section class="left">
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <article v-if="post" class="panel article">
        <VoteButtons :post="post" @changed="load" @error="error = $event" />
        <div class="l-container">
          <div class="meta">{{ post.author_name }} · {{ new Date(post.create_time).toLocaleString('zh-CN') }}</div>
          <h1 class="con-title">{{ post.title }}</h1>
          <p class="con-info">{{ post.content }}</p>
          <template v-if="session?.user_id === post.author_id || session?.role === 'admin'">
            <button class="delete-link" @click="confirmDialog.showModal()">删除帖子</button>
            <dialog ref="confirmDialog" class="delete-dialog" aria-labelledby="delete-title" aria-describedby="delete-message" @cancel="deleting && $event.preventDefault()">
              <h2 id="delete-title">删除帖子</h2>
              <p id="delete-message">确定删除《{{ post.title }}》吗？删除后无法恢复。</p>
              <div class="actions">
                <button class="secondary" :disabled="deleting" autofocus @click="confirmDialog.close()">取消</button>
                <button class="primary danger" :disabled="deleting" @click="deletePost">{{ deleting ? '删除中…' : '确认删除' }}</button>
              </div>
            </dialog>
          </template>
        </div>
      </article>
      <p v-else-if="!error" class="status" role="status">正在加载帖子…</p>
      <PostComments v-if="post" :post-id="post.id" />
      <RouterLink class="back-link" to="/">返回帖子列表</RouterLink>
    </section>
    <aside v-if="post" class="right panel">
      <h2 class="r-c-title">所属频道</h2>
      <div class="topic-info"><i class="avatar"></i> b/{{ post.community_name }}</div>
      <p class="topic-desc">{{ post.community.introduction }}</p>
      <RouterLink class="view-all" :to="{ path: '/', query: { community_id: post.community_id } }">查看频道帖子</RouterLink>
    </aside>
  </main>
</template>
