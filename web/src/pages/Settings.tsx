import { useState } from 'react'
import { setToken, clearToken } from '../api/client'

export default function Settings() {
  const [token, setTokenInput] = useState(
    () => localStorage.getItem('session_token') ?? '',
  )
  const [saved, setSaved] = useState(false)

  function save() {
    if (token.trim()) {
      setToken(token.trim())
    } else {
      clearToken()
    }
    setSaved(true)
    setTimeout(() => setSaved(false), 2000)
  }

  function logout() {
    clearToken()
    window.location.href = '/'
  }

  return (
    <div className="p-6 space-y-6 max-w-lg">
      <h1 className="text-2xl font-bold">Settings</h1>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Session</h2>
        <div className="space-y-1">
          <label className="text-sm text-gray-600">Session token</label>
          <input
            type="password"
            value={token}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder="Paste your Bearer token…"
            className="w-full border rounded-md px-3 py-2 text-sm"
          />
        </div>
        <div className="flex gap-3">
          <button
            onClick={save}
            className="px-4 py-2 bg-blue-600 text-white rounded-md text-sm hover:bg-blue-700"
          >
            {saved ? 'Saved!' : 'Save'}
          </button>
          <button
            onClick={logout}
            className="px-4 py-2 border rounded-md text-sm text-red-600 hover:bg-red-50"
          >
            Sign out
          </button>
        </div>
      </section>

      <section className="space-y-2">
        <h2 className="text-lg font-semibold">About</h2>
        <p className="text-sm text-gray-600">
          MediaMesh — decentralised media sharing between trusted nodes.
        </p>
      </section>
    </div>
  )
}
