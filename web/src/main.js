import { createApp, watch } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import Auth from './views/Auth.vue'
import Home from './views/Home.vue'
import Content from './views/Content.vue'
import Publish from './views/Publish.vue'
import ChangePassword from './views/ChangePassword.vue'
import ChangeUsername from './views/ChangeUsername.vue'
import NewCommunity from './views/NewCommunity.vue'
import { session } from './api'
import './style.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Home },
    { path: '/post/:id', component: Content, props: true },
    { path: '/publish', component: Publish },
    { path: '/change-password', component: ChangePassword, meta: { account: true } },
    { path: '/change-username', name: 'change-username', component: ChangeUsername, meta: { account: true } },
    { path: '/admin/communities/new', component: NewCommunity },
    { path: '/login', component: Auth },
    { path: '/signup', component: Auth, props: { signup: true } }
  ],
  // The discussion anchor is mounted after the post has loaded.
  scrollBehavior(to, from, savedPosition) { return savedPosition || (to.hash === '#comments' ? false : { top: 0 }) }
})
router.beforeEach(to => {
  if (session.value?.must_change_password && to.path !== '/change-password') return '/change-password'
  if (to.name === 'change-username' && session.value?.role === 'admin') return '/'
  if (to.path === '/admin/communities/new' && session.value?.role !== 'admin') return '/'
  if ((to.path === '/publish' || to.meta.account) && !session.value)
    return { path: '/login', query: { next: to.fullPath } }
})
watch(session, current => {
  const route = router.currentRoute.value
  if (current?.must_change_password && route.path !== '/change-password') router.replace('/change-password')
  else if (current?.role === 'admin' && route.name === 'change-username') router.replace('/')
  if (!current && route.meta.account) router.replace({ path: '/login', query: { next: route.fullPath } })
})
createApp(App).use(router).mount('#app')
