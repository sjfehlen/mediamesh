import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Pencil, Check, X } from 'lucide-react'
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

function LibraryRow({ lib }: { lib: Library }) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState({ name: lib.name, path: lib.path, media_type: lib.media_type })

  const update = useMutation({
    mutationFn: (body: typeof form) => api.patch(`/api/config/libraries/${lib.id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['config-libraries'] })
      qc.invalidateQueries({ queryKey: ['library'] })
      setEditing(false)
    },
  })

  const toggle = useMutation({
    mutationFn: () => api.patch(`/api/config/libraries/${lib.id}`, { enabled: !lib.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config-libraries'] }),
  })

  const scan = useMutation({
    mutationFn: () => api.post(`/api/config/libraries/${lib.id}/scan`),
  })

  const remove = useMutation({
    mutationFn: () => api.delete(`/api/config/libraries/${lib.id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['config-libraries'] })
      qc.invalidateQueries({ queryKey: ['library'] })
      qc.invalidateQueries({ queryKey: ['tv-series'] })
    },
  })

  if (editing) {
    return (
      <div className="px-4 py-3 space-y-3 bg-blue-50">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs text-gray-600 mb-1">Name</label>
            <input className="w-full border rounded px-2 py-1.5 text-sm" value={form.name}
              onChange={e => setForm(f => ({ ...f, name: e.target.value }))} />
          </div>
          <div>
            <label className="block text-xs text-gray-600 mb-1">Type</label>
            <select className="w-full border rounded px-2 py-1.5 text-sm bg-white"
              value={form.media_type} onChange={e => setForm(f => ({ ...f, media_type: e.target.value }))}>
              {MEDIA_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
          </div>
        </div>
        <div>
          <label className="block text-xs text-gray-600 mb-1">Path (inside container)</label>
          <input className="w-full border rounded px-2 py-1.5 text-sm font-mono" value={form.path}
            onChange={e => setForm(f => ({ ...f, path: e.target.value }))} />
        </div>
        <div className="flex gap-2">
          <button onClick={() => update.mutate(form)} disabled={update.isPending}
            className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
            <Check className="w-3.5 h-3.5" /> {update.isPending ? 'Saving…' : 'Save'}
          </button>
          <button onClick={() => { setEditing(false); setForm({ name: lib.name, path: lib.path, media_type: lib.media_type }) }}
            className="flex items-center gap-1 px-3 py-1.5 border rounded text-sm">
            <X className="w-3.5 h-3.5" /> Cancel
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium truncate">{lib.name}</p>
        <p className="text-xs text-gray-400 font-mono truncate">{lib.path}</p>
      </div>
      <span className="text-xs text-gray-500 bg-gray-100 px-2 py-0.5 rounded shrink-0">
        {lib.media_type}
      </span>
      <button onClick={() => toggle.mutate()}
        className={`text-xs px-2 py-0.5 rounded border shrink-0 ${
          lib.enabled ? 'text-green-700 border-green-200 bg-green-50' : 'text-gray-400 border-gray-200'}`}>
        {lib.enabled ? 'Enabled' : 'Disabled'}
      </button>
      <button onClick={() => scan.mutate()}
        className="text-xs text-blue-600 hover:underline shrink-0">
        {scan.isPending ? 'Scanning…' : 'Scan now'}
      </button>
      <button onClick={() => setEditing(true)}
        className="text-gray-400 hover:text-gray-700 shrink-0" title="Edit">
        <Pencil className="w-3.5 h-3.5" />
      </button>
      <button onClick={() => { if (confirm(`Remove library "${lib.name}"?`)) remove.mutate() }}
        className="text-xs text-red-500 hover:underline shrink-0">
        Remove
      </button>
    </div>
  )
}

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

  return (
    <div className="space-y-8 max-w-2xl">
      <h1 className="text-2xl font-bold">Settings</h1>

      {/* Libraries */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Media Libraries</h2>
            <p className="text-sm text-gray-500">Folders mounted into the container that MediaMesh will scan.</p>
          </div>
          <button onClick={() => setAdding(true)}
            className="px-3 py-1.5 bg-blue-600 text-white rounded-md text-sm hover:bg-blue-700">
            Add library
          </button>
        </div>

        {isLoading ? (
          <p className="text-sm text-gray-400">Loading…</p>
        ) : libraries.length === 0 ? (
          <p className="text-sm text-gray-400">No libraries configured.</p>
        ) : (
          <div className="border rounded-lg divide-y">
            {libraries.map(lib => <LibraryRow key={lib.id} lib={lib} />)}
          </div>
        )}

        {adding && (
          <div className="border rounded-lg p-4 space-y-3 bg-gray-50">
            <h3 className="text-sm font-semibold">Add library</h3>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs text-gray-600 mb-1">Name</label>
                <input className="w-full border rounded px-2 py-1.5 text-sm" placeholder="Movies"
                  value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))} />
              </div>
              <div>
                <label className="block text-xs text-gray-600 mb-1">Type</label>
                <select className="w-full border rounded px-2 py-1.5 text-sm bg-white"
                  value={form.media_type} onChange={e => setForm(f => ({ ...f, media_type: e.target.value }))}>
                  {MEDIA_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                </select>
              </div>
            </div>
            <div>
              <label className="block text-xs text-gray-600 mb-1">Path <span className="text-gray-400">(inside container)</span></label>
              <input className="w-full border rounded px-2 py-1.5 text-sm font-mono" placeholder="/media/movies"
                value={form.path} onChange={e => setForm(f => ({ ...f, path: e.target.value }))} />
            </div>
            {formError && <p className="text-red-600 text-xs">{formError}</p>}
            <div className="flex gap-2">
              <button onClick={() => createLib.mutate(form)} disabled={!form.name || !form.path}
                className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
                Add
              </button>
              <button onClick={() => { setAdding(false); setFormError('') }}
                className="px-3 py-1.5 border rounded text-sm">
                Cancel
              </button>
            </div>
          </div>
        )}
      </section>

      {/* About */}
      <section className="space-y-1">
        <h2 className="text-lg font-semibold">About</h2>
        <p className="text-sm text-gray-500">MediaMesh — decentralised media sharing between trusted nodes.</p>
      </section>
    </div>
  )
}
