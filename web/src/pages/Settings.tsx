import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'

interface Library {
  id: string
  name: string
  path: string
  media_type: string
  enabled: boolean
  created_at: string
}

const MEDIA_TYPES = ['movie', 'tv', 'audiobook', 'ebook']

export default function Settings() {
  const qc = useQueryClient()
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ name: '', path: '', media_type: 'movie' })
  const [formError, setFormError] = useState('')

  const { data: libraries = [], isLoading } = useQuery<Library[]>({
    queryKey: ['config-libraries'],
    queryFn: () => api.get('/api/config/libraries'),
  })

  const createLib = useMutation({
    mutationFn: (body: typeof form) => api.post('/api/config/libraries', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['config-libraries'] })
      setAdding(false)
      setForm({ name: '', path: '', media_type: 'movie' })
      setFormError('')
    },
    onError: (e: Error) => setFormError(e.message),
  })

  const toggleLib = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.patch(`/api/config/libraries/${id}`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config-libraries'] }),
  })

  const deleteLib = useMutation({
    mutationFn: (id: string) => api.delete(`/api/config/libraries/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['config-libraries'] })
      qc.invalidateQueries({ queryKey: ['library'] })
      qc.invalidateQueries({ queryKey: ['tv-series'] })
    },
  })

  const scanNow = useMutation({
    mutationFn: (id: string) => api.post(`/api/config/libraries/${id}/scan`),
  })

  return (
    <div className="space-y-8 max-w-2xl">
      <h1 className="text-2xl font-bold">Settings</h1>

      {/* Libraries */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Media Libraries</h2>
            <p className="text-sm text-gray-500">
              Folders mounted into the container that MediaMesh will scan.
            </p>
          </div>
          <button
            onClick={() => setAdding(true)}
            className="px-3 py-1.5 bg-blue-600 text-white rounded-md text-sm hover:bg-blue-700"
          >
            Add library
          </button>
        </div>

        {isLoading ? (
          <p className="text-sm text-gray-400">Loading…</p>
        ) : libraries.length === 0 ? (
          <p className="text-sm text-gray-400">No libraries configured.</p>
        ) : (
          <div className="border rounded-lg divide-y">
            {libraries.map(lib => (
              <div key={lib.id} className="flex items-center gap-4 px-4 py-3">
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{lib.name}</p>
                  <p className="text-xs text-gray-400 font-mono truncate">{lib.path}</p>
                </div>
                <span className="text-xs text-gray-500 bg-gray-100 px-2 py-0.5 rounded">
                  {lib.media_type}
                </span>
                <button
                  onClick={() => toggleLib.mutate({ id: lib.id, enabled: !lib.enabled })}
                  className={`text-xs px-2 py-0.5 rounded border ${
                    lib.enabled
                      ? 'text-green-700 border-green-200 bg-green-50'
                      : 'text-gray-400 border-gray-200'
                  }`}
                >
                  {lib.enabled ? 'Enabled' : 'Disabled'}
                </button>
                <button
                  onClick={() => scanNow.mutate(lib.id)}
                  className="text-xs text-blue-600 hover:underline"
                >
                  Scan now
                </button>
                <button
                  onClick={() => {
                    if (confirm(`Remove library "${lib.name}"?`)) {
                      deleteLib.mutate(lib.id)
                    }
                  }}
                  className="text-xs text-red-500 hover:underline"
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
        )}

        {adding && (
          <div className="border rounded-lg p-4 space-y-3 bg-gray-50">
            <h3 className="text-sm font-semibold">Add library</h3>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-gray-600 mb-1">Name</label>
                <input
                  className="w-full border rounded px-2 py-1.5 text-sm"
                  placeholder="Movies"
                  value={form.name}
                  onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                />
              </div>
              <div>
                <label className="block text-xs text-gray-600 mb-1">Type</label>
                <select
                  className="w-full border rounded px-2 py-1.5 text-sm bg-white"
                  value={form.media_type}
                  onChange={e => setForm(f => ({ ...f, media_type: e.target.value }))}
                >
                  {MEDIA_TYPES.map(t => (
                    <option key={t} value={t}>{t}</option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <label className="block text-xs text-gray-600 mb-1">
                Path <span className="text-gray-400">(inside container)</span>
              </label>
              <input
                className="w-full border rounded px-2 py-1.5 text-sm font-mono"
                placeholder="/media/movies"
                value={form.path}
                onChange={e => setForm(f => ({ ...f, path: e.target.value }))}
              />
            </div>
            {formError && <p className="text-red-600 text-xs">{formError}</p>}
            <div className="flex gap-2">
              <button
                onClick={() => createLib.mutate(form)}
                disabled={!form.name || !form.path}
                className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50"
              >
                Add
              </button>
              <button
                onClick={() => { setAdding(false); setFormError('') }}
                className="px-3 py-1.5 border rounded text-sm"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </section>

      {/* About */}
      <section className="space-y-1">
        <h2 className="text-lg font-semibold">About</h2>
        <p className="text-sm text-gray-500">
          MediaMesh — decentralised media sharing between trusted nodes.
        </p>
      </section>
    </div>
  )
}
