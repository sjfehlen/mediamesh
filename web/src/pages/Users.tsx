import { useEffect, useState } from 'react'
import { api, type User } from '../api/client'
import { clsx } from 'clsx'
import { UserX, UserCheck } from 'lucide-react'

export default function Users() {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    api
      .get<User[]>('/api/users')
      .then(setUsers)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  async function disable(id: string) {
    await api.delete(`/api/users/${id}`)
    setUsers((prev) =>
      prev.map((u) =>
        u.id === id ? { ...u, disabled_at: new Date().toISOString() } : u,
      ),
    )
  }

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Users</h1>
      {error && <p className="text-red-500">{error}</p>}
      {loading ? (
        <p className="text-gray-500">Loading…</p>
      ) : (
        <table className="w-full text-sm border-collapse">
          <thead>
            <tr className="border-b text-left">
              <th className="pb-2 pr-4">Username</th>
              <th className="pb-2 pr-4">Display Name</th>
              <th className="pb-2 pr-4">Role</th>
              <th className="pb-2 pr-4">Quota (GB)</th>
              <th className="pb-2 pr-4">Status</th>
              <th className="pb-2">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className="border-b hover:bg-gray-50">
                <td className="py-2 pr-4 font-mono text-xs">{u.username}</td>
                <td className="py-2 pr-4">{u.display_name}</td>
                <td className="py-2 pr-4">
                  <span
                    className={clsx(
                      'px-2 py-0.5 rounded-full text-xs font-medium',
                      u.role === 'admin'
                        ? 'bg-purple-100 text-purple-800'
                        : 'bg-gray-100 text-gray-700',
                    )}
                  >
                    {u.role}
                  </span>
                </td>
                <td className="py-2 pr-4 text-gray-500">
                  {u.quota_gb ?? '∞'}
                </td>
                <td className="py-2 pr-4">
                  {u.disabled_at ? (
                    <span className="text-red-500 flex items-center gap-1 text-xs">
                      <UserX className="w-3 h-3" /> Disabled
                    </span>
                  ) : (
                    <span className="text-green-600 flex items-center gap-1 text-xs">
                      <UserCheck className="w-3 h-3" /> Active
                    </span>
                  )}
                </td>
                <td className="py-2">
                  {!u.disabled_at && (
                    <button
                      onClick={() => disable(u.id)}
                      className="text-red-500 hover:underline text-xs"
                    >
                      Disable
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
