import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { clsx } from 'clsx'
import { api, type Request } from '../api/client'

const statusColors: Record<string, string> = {
  pending:  'bg-yellow-100 text-yellow-800',
  approved: 'bg-green-100 text-green-800',
  rejected: 'bg-red-100 text-red-800',
}

function RequestActions({ request }: { request: Request }) {
  const qc = useQueryClient()

  const approve = useMutation({
    mutationFn: () => api.post(`/api/requests/${request.id}/approve`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['requests'] }),
  })
  const reject = useMutation({
    mutationFn: () => api.post(`/api/requests/${request.id}/reject`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['requests'] }),
  })
  const cancel = useMutation({
    mutationFn: () => api.delete(`/api/requests/${request.id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['requests'] }),
  })

  if (request.status !== 'pending') {
    return (
      <button onClick={() => cancel.mutate()} className="text-gray-400 hover:text-red-500 text-xs">
        Remove
      </button>
    )
  }

  return (
    <div className="flex gap-2">
      <button onClick={() => approve.mutate()} disabled={approve.isPending}
        className="text-green-600 hover:underline text-xs disabled:opacity-50">
        Approve
      </button>
      <button onClick={() => reject.mutate()} disabled={reject.isPending}
        className="text-red-600 hover:underline text-xs disabled:opacity-50">
        Reject
      </button>
    </div>
  )
}

export default function Requests() {
  const qc = useQueryClient()
  const [title, setTitle] = useState('')
  const [note, setNote] = useState('')

  // Only show want-list requests (no item_id = media nobody in the mesh has)
  const { data: allRequests = [], isLoading, error } = useQuery({
    queryKey: ['requests'],
    queryFn: () => api.get<Request[]>('/api/requests'),
  })
  const requests = allRequests.filter((r) => !r.item_id)

  const submit = useMutation({
    mutationFn: () => api.post('/api/requests', { note: `${title}${note ? ` — ${note}` : ''}` }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['requests'] })
      setTitle('')
      setNote('')
    },
  })

  return (
    <div className="p-6 space-y-6 max-w-2xl">
      <h1 className="text-2xl font-bold">Media Requests</h1>
      <p className="text-sm text-gray-500">
        Use this page to request media that nobody in the mesh currently has.
        To download something a peer already has, click it in the Library and use
        the <strong>Request transfer</strong> button — that transfers automatically.
      </p>

      {/* Submit form */}
      <div className="bg-gray-50 border rounded-lg p-4 space-y-3">
        <p className="text-sm font-medium">Request new media</p>
        <input
          type="text"
          placeholder="Title (e.g. The Dark Knight)"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          className="w-full border rounded px-3 py-2 text-sm"
        />
        <input
          type="text"
          placeholder="Optional note (year, format, season…)"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="w-full border rounded px-3 py-2 text-sm"
        />
        <button
          onClick={() => submit.mutate()}
          disabled={submit.isPending || !title.trim()}
          className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50"
        >
          {submit.isPending ? 'Submitting…' : 'Submit request'}
        </button>
      </div>

      {error && <p className="text-red-500">{(error as Error).message}</p>}

      {isLoading ? (
        <p className="text-gray-500">Loading…</p>
      ) : requests.length === 0 ? (
        <p className="text-gray-500">No open media requests.</p>
      ) : (
        <table className="w-full text-sm border-collapse">
          <thead>
            <tr className="border-b text-left text-xs text-gray-500">
              <th className="pb-2 pr-4">Request</th>
              <th className="pb-2 pr-4">Status</th>
              <th className="pb-2 pr-4">Date</th>
              <th className="pb-2" />
            </tr>
          </thead>
          <tbody>
            {requests.map((req) => (
              <tr key={req.id} className="border-b hover:bg-gray-50">
                <td className="py-2 pr-4">{req.note ?? '—'}</td>
                <td className="py-2 pr-4">
                  <span className={clsx('px-2 py-0.5 rounded-full text-xs font-medium',
                    statusColors[req.status] ?? 'bg-gray-100 text-gray-700')}>
                    {req.status}
                  </span>
                </td>
                <td className="py-2 pr-4 text-xs text-gray-500">
                  {new Date(req.requested_at).toLocaleDateString()}
                </td>
                <td className="py-2">
                  <RequestActions request={req} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
