import { useEffect, useState } from 'react'
import { api, type AuditEntry } from '../api/client'

export default function AuditLog() {
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actorFilter, setActorFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')

  useEffect(() => {
    setLoading(true)
    const params = new URLSearchParams()
    if (actorFilter) params.set('actor_id', actorFilter)
    if (actionFilter) params.set('action', actionFilter)
    params.set('limit', '200')

    api
      .get<AuditEntry[]>(`/api/audit?${params}`)
      .then(setEntries)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [actorFilter, actionFilter])

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Audit Log</h1>

      <div className="flex gap-3 flex-wrap">
        <input
          type="text"
          placeholder="Filter by actor ID…"
          value={actorFilter}
          onChange={(e) => setActorFilter(e.target.value)}
          className="border rounded-md px-3 py-2 text-sm w-56"
        />
        <input
          type="text"
          placeholder="Filter by action…"
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          className="border rounded-md px-3 py-2 text-sm w-56"
        />
      </div>

      {error && <p className="text-red-500">{error}</p>}

      {loading ? (
        <p className="text-gray-500">Loading…</p>
      ) : (
        <table className="w-full text-sm border-collapse">
          <thead>
            <tr className="border-b text-left">
              <th className="pb-2 pr-4">Time</th>
              <th className="pb-2 pr-4">Actor</th>
              <th className="pb-2 pr-4">Action</th>
              <th className="pb-2 pr-4">Target</th>
              <th className="pb-2">Detail</th>
            </tr>
          </thead>
          <tbody>
            {entries.length === 0 ? (
              <tr>
                <td colSpan={5} className="pt-4 text-gray-500">
                  No entries.
                </td>
              </tr>
            ) : (
              entries.map((e) => (
                <tr key={e.id} className="border-b hover:bg-gray-50 text-xs">
                  <td className="py-1.5 pr-4 text-gray-500 whitespace-nowrap">
                    {new Date(e.occurred_at).toLocaleString()}
                  </td>
                  <td className="py-1.5 pr-4 font-mono">
                    {e.actor_id ? `${e.actor_id.slice(0, 8)}…` : e.actor_type}
                  </td>
                  <td className="py-1.5 pr-4 font-medium">{e.action}</td>
                  <td className="py-1.5 pr-4 text-gray-600">
                    {e.target_type && `${e.target_type}/${e.target_id?.slice(0, 8)}`}
                  </td>
                  <td className="py-1.5 text-gray-500 truncate max-w-xs">{e.detail}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}
    </div>
  )
}
