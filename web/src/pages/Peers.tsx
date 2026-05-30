import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { clsx } from 'clsx'
import { Wifi, WifiOff, Copy, Check, Pencil } from 'lucide-react'
import { api, type Peer } from '../api/client'

export default function Peers() {
  const qc = useQueryClient()
  const [invite, setInvite] = useState<{ url: string; token: string } | null>(null)
  const [copied, setCopied] = useState<'url' | 'token' | null>(null)
  const [redeemUrl, setRedeemUrl] = useState('')
  const [redeemToken, setRedeemToken] = useState('')
  const [redeemError, setRedeemError] = useState<string | null>(null)
  const [editingPeer, setEditingPeer] = useState<string | null>(null)
  const [editEndpoint, setEditEndpoint] = useState('')

  const { data: peers = [], isLoading, error } = useQuery({
    queryKey: ['peers'],
    queryFn: () => api.get<Peer[]>('/api/peers'),
  })

  const generateInvite = useMutation({
    mutationFn: () => api.post<{ url: string; token: string }>('/api/peers/invite'),
    onSuccess: (res) => setInvite(res),
  })

  const redeemInvite = useMutation({
    mutationFn: () => api.post<Peer>('/api/peers/accept', {
      token: redeemToken.trim(),
      endpoint: redeemUrl.trim() || undefined,
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['peers'] })
      setRedeemUrl('')
      setRedeemToken('')
      setRedeemError(null)
    },
    onError: (err: Error) => setRedeemError(err.message),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => api.delete(`/api/peers/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['peers'] }),
  })

  const sync = useMutation({
    mutationFn: (id: string) => api.post(`/api/peers/${id}/sync`),
  })

  const rehandshake = useMutation({
    mutationFn: (id: string) => api.post(`/api/peers/${id}/rehandshake`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['peers'] }),
  })

  const patchEndpoint = useMutation({
    mutationFn: ({ id, endpoint }: { id: string; endpoint: string }) =>
      api.patch(`/api/peers/${id}`, { endpoint }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['peers'] })
      setEditingPeer(null)
    },
  })

  function handleRevoke(id: string) {
    if (!confirm('Remove this peer?')) return
    revoke.mutate(id)
  }

  function copyField(field: 'url' | 'token', value: string) {
    navigator.clipboard.writeText(value)
    setCopied(field)
    setTimeout(() => setCopied(null), 2000)
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Peers</h1>
        <button
          onClick={() => generateInvite.mutate()}
          disabled={generateInvite.isPending}
          className="px-4 py-2 bg-blue-600 text-white rounded-md text-sm hover:bg-blue-700 disabled:opacity-50"
        >
          Generate Invite
        </button>
      </div>

      {invite && (
        <div className="bg-blue-50 border border-blue-200 rounded-md p-4 space-y-3 text-sm">
          <p className="font-medium text-blue-800">Share both fields with the other node:</p>
          <div className="space-y-2">
            <div>
              <label className="block text-xs text-blue-700 mb-1">Server URL</label>
              <div className="flex gap-2">
                <code className="flex-1 text-xs break-all bg-white border rounded px-2 py-1.5 select-all">
                  {invite.url}
                </code>
                <button
                  onClick={() => copyField('url', invite.url)}
                  className="shrink-0 p-1.5 rounded border bg-white hover:bg-gray-50"
                  title="Copy URL"
                >
                  {copied === 'url' ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5 text-gray-500" />}
                </button>
              </div>
            </div>
            <div>
              <label className="block text-xs text-blue-700 mb-1">Invite Code</label>
              <div className="flex gap-2">
                <code className="flex-1 text-xs break-all bg-white border rounded px-2 py-1.5 select-all font-mono">
                  {invite.token}
                </code>
                <button
                  onClick={() => copyField('token', invite.token)}
                  className="shrink-0 p-1.5 rounded border bg-white hover:bg-gray-50"
                  title="Copy code"
                >
                  {copied === 'token' ? <Check className="w-3.5 h-3.5 text-green-500" /> : <Copy className="w-3.5 h-3.5 text-gray-500" />}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="bg-gray-50 border border-gray-200 rounded-md p-4 space-y-3">
        <p className="text-sm font-medium text-gray-700">Connect to a peer using their invite:</p>
        <div className="space-y-2">
          <div>
            <label className="block text-xs text-gray-600 mb-1">Their Server URL <span className="text-red-500">*</span></label>
            <input
              type="text"
              value={redeemUrl}
              onChange={(e) => { setRedeemUrl(e.target.value); setRedeemError(null) }}
              placeholder="https://mediamesh.theirserver.com"
              className="w-full text-sm border rounded px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <p className="text-xs text-gray-400 mt-0.5">Must be reachable from this server — use an IP or local hostname if the domain isn't set up yet.</p>
          </div>
          <div>
            <label className="block text-xs text-gray-600 mb-1">Invite Code</label>
            <input
              type="text"
              value={redeemToken}
              onChange={(e) => { setRedeemToken(e.target.value); setRedeemError(null) }}
              placeholder="Paste invite code…"
              className="w-full text-xs border rounded px-3 py-2 font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
        </div>
        <button
          onClick={() => redeemInvite.mutate()}
          disabled={redeemInvite.isPending || !redeemToken.trim() || !redeemUrl.trim()}
          className="px-4 py-2 bg-green-600 text-white rounded-md text-sm hover:bg-green-700 disabled:opacity-50"
        >
          {redeemInvite.isPending ? 'Connecting…' : 'Connect to Peer'}
        </button>
        {redeemError && <p className="text-xs text-red-600">{redeemError}</p>}
      </div>

      {error && <p className="text-red-500">{(error as Error).message}</p>}

      {isLoading ? (
        <p className="text-gray-500">Loading…</p>
      ) : peers.length === 0 ? (
        <p className="text-gray-500">No peers connected.</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {peers.map((p) => (
            <div key={p.id} className="bg-white rounded-lg shadow p-4 space-y-2">
              <div className="flex items-center justify-between">
                <span className="font-medium">{p.display_name}</span>
                {p.status === 'active' ? (
                  <Wifi className="w-4 h-4 text-green-500" />
                ) : (
                  <WifiOff className="w-4 h-4 text-gray-400" />
                )}
              </div>
              {editingPeer === p.id ? (
                <div className="flex gap-1">
                  <input
                    type="text"
                    value={editEndpoint}
                    onChange={(e) => setEditEndpoint(e.target.value)}
                    className="flex-1 text-xs border rounded px-2 py-1 font-mono"
                    autoFocus
                  />
                  <button
                    onClick={() => patchEndpoint.mutate({ id: p.id, endpoint: editEndpoint })}
                    className="text-xs text-green-600 hover:underline"
                  >Save</button>
                  <button
                    onClick={() => setEditingPeer(null)}
                    className="text-xs text-gray-400 hover:underline"
                  >Cancel</button>
                </div>
              ) : (
                <div className="flex items-center gap-1">
                  <p className="text-xs text-gray-500 break-all flex-1">{p.endpoint}</p>
                  <button
                    onClick={() => { setEditingPeer(p.id); setEditEndpoint(p.endpoint) }}
                    className="shrink-0 text-gray-300 hover:text-gray-500"
                    title="Edit endpoint"
                  >
                    <Pencil className="w-3 h-3" />
                  </button>
                </div>
              )}
              <div className="flex items-center justify-between text-xs">
                <span
                  className={clsx(
                    'px-2 py-0.5 rounded-full font-medium',
                    p.status === 'active'
                      ? 'bg-green-100 text-green-700'
                      : 'bg-gray-100 text-gray-600',
                  )}
                >
                  {p.status}
                </span>
                <div className="flex gap-3">
                  {p.status === 'active' && (
                    <button
                      onClick={() => sync.mutate(p.id)}
                      disabled={sync.isPending}
                      className="text-blue-500 hover:underline disabled:opacity-50"
                    >
                      {sync.isPending ? 'Syncing…' : 'Sync Now'}
                    </button>
                  )}
                  <button
                    onClick={() => rehandshake.mutate(p.id)}
                    disabled={rehandshake.isPending}
                    className="text-yellow-600 hover:underline disabled:opacity-50"
                    title="Re-send handshake — use if the remote peer lost its DB"
                  >
                    {rehandshake.isPending ? 'Registering…' : 'Re-register'}
                  </button>
                  <button
                    onClick={() => handleRevoke(p.id)}
                    disabled={revoke.isPending}
                    className="text-red-500 hover:underline disabled:opacity-50"
                  >
                    Remove
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
