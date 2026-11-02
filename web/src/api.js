import { ref } from 'vue'

function readSession() {
  try {
    const saved = JSON.parse(localStorage.getItem('loginResult'))
    return saved?.token && saved?.user_id && saved?.user_name ? saved : null
  } catch {
    return null
  }
}

export const session = ref(readSession())
export const sessionNotice = ref('')
window.addEventListener('storage', () => { session.value = readSession() })

function writeSession(data) {
  if (data) localStorage.setItem('loginResult', JSON.stringify(data))
  else localStorage.removeItem('loginResult')
  session.value = data
}

// Serialize response handling across tabs before comparing and writing storage.
function updateSession(operation) {
  if (navigator.locks) return navigator.locks.request('forum-session', operation)
  return Promise.resolve(operation())
}

export function setSession(data) {
  return updateSession(() => {
    writeSession(data)
    sessionNotice.value = ''
  })
}

function claims(token) {
  try {
    return JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return null
  }
}

function sameSession(first, second) {
  if (!first || !second) return false
  if (first === second) return true
  const sid = claims(first)?.sid
  return Boolean(sid && sid === claims(second)?.sid)
}

export async function logout() {
  const token = readSession()?.token
  try {
    await request('logout', undefined, { method: 'POST', token: token ?? null })
  } catch (err) {
    if (err.status !== 401) throw err
  }
  await clearSession(token)
}

function clearSession(token) {
  return updateSession(() => {
    if (sameSession(token, readSession()?.token)) writeSession(null)
  })
}

export async function changeInitialPassword(password, confirmation) {
  const token = readSession()?.token ?? null
  await request('me/password', { new_password: password, re_password: confirmation }, { token })
  await clearSession(token)
}

export async function changeUsername(username) {
  const token = readSession()?.token ?? null
  const data = await request('me/username', { username }, { method: 'PUT', token })
  await updateSession(() => {
    const current = readSession()
    if (sameSession(token, current?.token)) writeSession({ ...current, user_name: data.username })
  })
  return data.username
}

export async function request(path, body, { signal, method = body ? 'POST' : 'GET', token = readSession()?.token } = {}) {
  const headers = { Accept: 'application/json' }
  session.value = readSession()
  if (token) headers.Authorization = 'Bearer ' + token
  if (body) headers['Content-Type'] = 'application/json'
  let response
  try {
    response = await fetch('/api/v1/' + path, {
      method, headers, body: body ? JSON.stringify(body) : undefined, signal
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw new Error('网络连接失败，请重试。')
  }
  const result = await response.json().catch(() => null)
  const renewed = response.headers.get('X-Renewed-Token')
  await updateSession(() => {
    const current = readSession()
    // An expired request must not clear a token already renewed by another request.
    if (response.status === 401 && result?.code !== 1004 && token && token === current?.token) {
      writeSession(null)
      sessionNotice.value = result?.msg || '登录已失效，请重新登录'
    } else if (response.status === 403 && result?.code === 1012 && sameSession(token, current?.token)) {
      writeSession({ ...current, must_change_password: true })
    } else if (response.status !== 401 && renewed && path !== 'logout' && sameSession(token, current?.token) &&
      sameSession(renewed, current.token) && claims(renewed)?.exp > claims(current.token)?.exp) {
      writeSession({ ...current, token: renewed })
    } else {
      session.value = current
    }
  })
  if (!response.ok || result?.code !== 1000) {
    const error = new Error(result?.msg || '请求失败（HTTP ' + response.status + '）')
    error.status = response.status
    throw error
  }
  return result.data
}
