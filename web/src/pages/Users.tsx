import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
} from '@tanstack/react-table'
import { useState } from 'react'
import { clsx } from 'clsx'
import { UserX, UserCheck } from 'lucide-react'
import { api, type User } from '../api/client'

const columnHelper = createColumnHelper<User>()

function UserActions({ user }: { user: User }) {
  const qc = useQueryClient()
  const disable = useMutation({
    mutationFn: () => api.delete(`/api/users/${user.id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  })

  if (user.disabled_at) return null

  return (
    <button
      onClick={() => disable.mutate()}
      disabled={disable.isPending}
      className="text-red-500 hover:underline text-xs disabled:opacity-50"
    >
      Disable
    </button>
  )
}

const columns = [
  columnHelper.accessor('username', {
    header: 'Username',
    cell: (info) => <span className="font-mono text-xs">{info.getValue()}</span>,
  }),
  columnHelper.accessor('display_name', {
    header: 'Display Name',
  }),
  columnHelper.accessor('role', {
    header: 'Role',
    cell: (info) => (
      <span
        className={clsx(
          'px-2 py-0.5 rounded-full text-xs font-medium',
          info.getValue() === 'admin'
            ? 'bg-purple-100 text-purple-800'
            : 'bg-gray-100 text-gray-700',
        )}
      >
        {info.getValue()}
      </span>
    ),
  }),
  columnHelper.accessor('quota_gb', {
    header: 'Quota (GB)',
    cell: (info) => <span className="text-gray-500">{info.getValue() ?? '∞'}</span>,
  }),
  columnHelper.accessor('disabled_at', {
    header: 'Status',
    cell: (info) =>
      info.getValue() ? (
        <span className="text-red-500 flex items-center gap-1 text-xs">
          <UserX className="w-3 h-3" /> Disabled
        </span>
      ) : (
        <span className="text-green-600 flex items-center gap-1 text-xs">
          <UserCheck className="w-3 h-3" /> Active
        </span>
      ),
  }),
  columnHelper.display({
    id: 'actions',
    header: 'Actions',
    cell: ({ row }) => <UserActions user={row.original} />,
    enableSorting: false,
  }),
]

export default function Users() {
  const [sorting, setSorting] = useState<SortingState>([])

  const { data: users = [], isLoading, error } = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<User[]>('/api/users'),
  })

  const table = useReactTable({
    data: users,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Users</h1>
      {error && <p className="text-red-500">{(error as Error).message}</p>}
      {isLoading ? (
        <p className="text-gray-500">Loading…</p>
      ) : (
        <table className="w-full text-sm border-collapse">
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b text-left">
                {hg.headers.map((header) => (
                  <th
                    key={header.id}
                    className="pb-2 pr-4 select-none cursor-pointer"
                    onClick={header.column.getToggleSortingHandler()}
                  >
                    {flexRender(header.column.columnDef.header, header.getContext())}
                    {header.column.getIsSorted() === 'asc'
                      ? ' ↑'
                      : header.column.getIsSorted() === 'desc'
                        ? ' ↓'
                        : ''}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="pt-4 text-gray-500">
                  No users.
                </td>
              </tr>
            ) : (
              table.getRowModel().rows.map((row) => (
                <tr key={row.id} className="border-b hover:bg-gray-50">
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="py-2 pr-4">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}
    </div>
  )
}
