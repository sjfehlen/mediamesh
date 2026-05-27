import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
} from '@tanstack/react-table'
import { useState } from 'react'
import { clsx } from 'clsx'
import { api, type Request } from '../api/client'

const statusColors: Record<string, string> = {
  pending: 'bg-yellow-100 text-yellow-800',
  approved: 'bg-green-100 text-green-800',
  rejected: 'bg-red-100 text-red-800',
}

const columnHelper = createColumnHelper<Request>()

const columns = [
  columnHelper.accessor('item_id', {
    header: 'Item',
    cell: (info) => (
      <span className="font-mono text-xs">{info.getValue().slice(0, 8)}…</span>
    ),
  }),
  columnHelper.accessor('status', {
    header: 'Status',
    cell: (info) => (
      <span
        className={clsx(
          'px-2 py-0.5 rounded-full text-xs font-medium',
          statusColors[info.getValue()] ?? 'bg-gray-100 text-gray-700',
        )}
      >
        {info.getValue()}
      </span>
    ),
  }),
  columnHelper.accessor('note', {
    header: 'Note',
    cell: (info) => info.getValue() ?? '—',
    enableSorting: false,
  }),
  columnHelper.accessor('requested_at', {
    header: 'Requested',
    cell: (info) => new Date(info.getValue()).toLocaleDateString(),
  }),
  columnHelper.display({
    id: 'actions',
    header: 'Actions',
    cell: ({ row }) => <RequestActions request={row.original} />,
  }),
]

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

  if (request.status !== 'pending') return null

  return (
    <div className="flex gap-2">
      <button
        onClick={() => approve.mutate()}
        disabled={approve.isPending}
        className="text-green-600 hover:underline text-xs disabled:opacity-50"
      >
        Approve
      </button>
      <button
        onClick={() => reject.mutate()}
        disabled={reject.isPending}
        className="text-red-600 hover:underline text-xs disabled:opacity-50"
      >
        Reject
      </button>
    </div>
  )
}

export default function Requests() {
  const [sorting, setSorting] = useState<SortingState>([])
  const [globalFilter, setGlobalFilter] = useState('')

  const { data: requests = [], isLoading, error } = useQuery({
    queryKey: ['requests'],
    queryFn: () => api.get<Request[]>('/api/requests'),
  })

  const table = useReactTable({
    data: requests,
    columns,
    state: { sorting, globalFilter },
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Requests</h1>
        <input
          value={globalFilter}
          onChange={(e) => setGlobalFilter(e.target.value)}
          placeholder="Filter…"
          className="border rounded px-3 py-1 text-sm w-48"
        />
      </div>

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
                    className="pb-2 pr-4 select-none"
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
                  No requests.
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
