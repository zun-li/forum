<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { request, session } from '../api'
import VoteButtons from '../components/VoteButtons.vue'

const route = useRoute()
const router = useRouter()
const communities = ref([])
const communityError = ref('')
const posts = ref([])
const page = computed(() => {
  const n = Number(route.query.page)
  return Number.isSafeInteger(n) && n > 0 ? n : 1
})
const revision = ref(0)
const loading = ref(true)
const postDeleted = history.state?.postDeleted === true
const error = ref('')
const size = 10

onMounted(async () => {
  if (postDeleted) history.replaceState({ ...history.state, postDeleted: false }, '')
  try { communities.value = await request('communities') }
  catch (e) { communityError.value = e.message }
})

function filter(query) {
  router.push({ path: '/', query: { ...route.query, page: undefined, ...query } })
}

watch([() => route.query, revision], async (_, __, onCleanup) => {
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ page: page.value, size, order: route.query.order || 'time' })
    if (route.query.community_id) params.set('community_id', route.query.community_id)
    posts.value = await request('posts?' + params, undefined, { signal: controller.signal })
  } catch (e) {
    if (!controller.signal.aborted) { error.value = e.message; posts.value = [] }
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}, { immediate: true })
</script>

<template>
  <main class="content">
    <section class="left" aria-label="帖子列表">
      <p v-if="postDeleted" class="notice" role="status">帖子已删除</p>
      <div class="panel c-l-header">
        <button class="new" :class="{ active: route.query.order !== 'score' }" @click="filter({ order: 'time' })"><i class="iconfont icon-polygonred"></i> New</button>
        <button class="top" :class="{ active: route.query.order === 'score' }" @click="filter({ order: 'score' })"><i class="iconfont icon-top"></i> Score</button>
        <RouterLink class="btn-publish" to="/publish">发表</RouterLink>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="loading" class="status" role="status">正在加载帖子…</p>
      <p v-else-if="!posts.length && !error" class="panel status">这一页还没有帖子。</p>
      <ul class="c-l-list">
        <li v-for="post in posts" :key="post.id" class="panel c-l-item">
          <VoteButtons :post="post" @changed="revision++" @error="error = $event" />
          <div class="l-container">
            <RouterLink class="post-preview" :to="'/post/' + post.id">
              <div class="meta">b/{{ post.community_name }} · {{ post.author_name }}</div>
              <h2 class="con-title">{{ post.title }}</h2>
              <p class="con-memo">{{ post.content }}</p>
            </RouterLink>
            <RouterLink class="comment-link" :to="{ path: '/post/' + post.id, hash: '#comments' }">{{ post.comment_count }} 条留言</RouterLink>
          </div>
        </li>
      </ul>
      <nav v-if="page > 1 || posts.length === size" class="pagination" aria-label="帖子翻页">
        <button :disabled="page === 1 || loading" @click="filter({ page: page - 1 })">上一页</button>
        <span>第 {{ page }} 页</span>
        <button :disabled="posts.length < size || loading" @click="filter({ page: page + 1 })">下一页</button>
      </nav>
    </section>
    <aside class="right panel communities">
      <h2 class="r-c-title">社区频道</h2>
      <RouterLink v-if="session?.role === 'admin'" class="view-all" to="/admin/communities/new">新建频道</RouterLink>
      <p v-if="communityError" class="error" role="alert">{{ communityError }}</p>
      <ul>
        <li v-for="(community, i) in communities" :key="community.id">
          <button class="r-c-item" :class="{ selected: String(community.id) === route.query.community_id }" @click="filter({ community_id: String(community.id) })">
            <span class="index">{{ i + 1 }}</span><i class="avatar"></i>b/{{ community.name }}
          </button>
        </li>
      </ul>
      <button class="view-all" @click="filter({ community_id: undefined })">查看所有帖子</button>
    </aside>
  </main>
</template>
