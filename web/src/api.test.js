import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'

const source = readFileSync(new URL('./api.js', import.meta.url), 'utf8')
  .replace("import { ref } from 'vue'", '')
  .replaceAll('export ', '')

function browserStorage() {
  const values = new Map()
  let pending = Promise.resolve()
  return {
    localStorage: {
      getItem: key => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
      removeItem: key => values.delete(key)
    },
    locks: {
      request(name, operation) {
        const result = pending.then(operation)
        pending = result.catch(() => {})
        return result
      }
    }
  }
}

function client(storage, fetch) {
  return runInNewContext(source + '\n;({ request, setSession, logout, session, sessionNotice })', {
    ref: value => ({ value }),
    localStorage: storage.localStorage,
    navigator: { locks: storage.locks },
    window: { addEventListener() {} },
    fetch, atob
  })
}

function token(sid, exp) {
  return 'header.' + Buffer.from(JSON.stringify({ sid, exp })).toString('base64url') + '.signature'
}

function login(sid = 'first', exp = 100) {
  return { user_id: '7', user_name: 'alice', token: token(sid, exp) }
}

function response(status = 200, renewed, code = 1000) {
  const headers = renewed ? { 'X-Renewed-Token': renewed } : {}
  return new Response(JSON.stringify({ code, msg: status === 401 ? '账号已在其他设备登录' : '服务繁忙', data: [] }), { status, headers })
}

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

function saved(storage) {
  return JSON.parse(storage.localStorage.getItem('loginResult'))
}

test('out-of-order renewal responses across tabs keep the latest expiry', async () => {
  const storage = browserStorage()
  const slow = deferred()
  const fast = deferred()
  const first = client(storage, () => slow.promise)
  const second = client(storage, () => fast.promise)
  await first.setSession(login())
  const earlier = first.request('posts')
  const later = second.request('posts')
  fast.resolve(response(200, token('first', 300)))
  await later
  slow.resolve(response(200, token('first', 200)))
  await earlier
  assert.equal(saved(storage).token, token('first', 300))
  assert.equal(first.session.value.token, token('first', 300))
})

test('an expired in-flight request cannot clear a renewed token', async () => {
  const storage = browserStorage()
  const slow = deferred()
  const api = client(storage, () => slow.promise)
  await api.setSession(login())
  const old = api.request('me')
  await api.setSession(login('first', 200))
  slow.resolve(response(401, undefined, 1007))
  await assert.rejects(old)
  assert.equal(saved(storage).token, token('first', 200))
})

test('a late renewal cannot restore a logged-out session', async () => {
  const storage = browserStorage()
  const slow = deferred()
  const api = client(storage, path => path.endsWith('/logout') ? Promise.resolve(response()) : slow.promise)
  await api.setSession(login())
  const inFlight = api.request('posts')
  await api.logout()
  assert.equal(saved(storage), null)
  slow.resolve(response(200, token('first', 200)))
  await inFlight
  assert.equal(saved(storage), null)
})

test('an old response cannot renew or log out a newer login', async () => {
  const storage = browserStorage()
  const renewal = deferred()
  const logout = deferred()
  const api = client(storage, path => path.endsWith('/logout') ? logout.promise : renewal.promise)
  await api.setSession(login())
  const inFlight = api.request('posts')
  const signingOut = api.logout()
  await api.setSession(login('second', 400))
  renewal.resolve(response(200, token('first', 500)))
  logout.resolve(response())
  await Promise.all([inFlight, signingOut])
  assert.equal(saved(storage).token, token('second', 400))
})

test('session and logout storage failures preserve the local login', async () => {
  const storage = browserStorage()
  const api = client(storage, async () => response(500, undefined, 1005))
  await api.setSession(login())
  await assert.rejects(api.request('me'))
  await assert.rejects(api.logout())
  assert.equal(saved(storage).token, login().token)
})

test('replaced sessions clear storage and expose the server message', async () => {
  const storage = browserStorage()
  const api = client(storage, async () => response(401, undefined, 1007))
  await api.setSession(login())
  await assert.rejects(api.request('posts'))
  assert.equal(saved(storage), null)
  assert.equal(api.sessionNotice.value, '账号已在其他设备登录')
})

test('incorrect login credentials do not clear an existing session', async () => {
  const storage = browserStorage()
  const api = client(storage, async () => response(401, undefined, 1004))
  await api.setSession(login())
  await assert.rejects(api.request('login', { username: 'alice', password: 'wrong' }))
  assert.equal(saved(storage).token, login().token)
})
