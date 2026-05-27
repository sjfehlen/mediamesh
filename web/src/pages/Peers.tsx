import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { clsx } from 'clsx'
import { Wifi, WifiOff } from 'lucide-react'
import { api, type Peer } from '../api/client'

export default function Peers() {
  const qc = useQueryClient()
  const [inviteToken, setInviteToken] = useState<string | null>(null)

  const { data: peers = [], isLoading, error } = useQuery({
    queryKey: ['peers'],
    queryFn: () => api.get<Peer[]>('/api/peers'),
  })

  const generateInvite = useMutation({
    mutationFn: () => api.post<{ token: string }>('/api/peers/invite'),
    onSuccess: (res) => setInviteToken(res.token),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => api.delete(`/api/peers/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['peers'] }),
  })

  function handleRevoke(id: string) {
    if (!confirm('Revoke this peer?')) return
    revoke.mutate(id)
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

      {inviteToken && (
        <div className="bg-blue-50 border border-blue-200 rounded-md p-4 text-sm space-y-1">
          <p className="font-medium text-blue-800">Peer invite token (share with the other node):</p>
          <code className="block text-xs break-all bg-white border rounded p-2 select-all">
            {inviteToken}
          </code>
        </div>
      )}

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
              <p className="text-xs text-gray-500 break-all">{p.endpoint}</p>
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
                {p.status === 'active' && (
                  <button
                    onClick={() => handleRevoke(p.id)}
                    disabled={revoke.isPending}
                    className="text-red-500 hover:underline disabled:opacity-50"
                  >
                    Revoke
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
