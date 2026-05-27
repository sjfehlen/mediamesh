import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
} from '@tanstack/react-table'
import { api, type AuditEntry } from '../api/client'

const columnHelper = createColumnHelper<AuditEntry>()

const columns = [
  columnHelper.accessor('occurred_at', {
    header: 'Time',
    cell: (info) => (
      <span className="whitespace-nowrap text-gray-500">
        {new Date(info.getValue()).toLocaleString()}
      </span>
    ),
  }),
  columnHelper.display({
    id: 'actor',
    header: 'Actor',
    cell: ({ row }) => (
      <span className="font-mono">
        {row.original.actor_id
          ? `${row.original.actor_id.slice(0, 8)}…`
          : row.original.actor_type}
      </span>
    ),
    enableSorting: false,
  }),
  columnHelper.accessor('action', {
    header: 'Action',
    cell: (info) => <span className="font-medium">{info.getValue()}</span>,
  }),
  columnHelper.display({
    id: 'target',
    header: 'Target',
    cell: ({ row }) =>
      row.original.target_type
        ? `${row.original.target_type}/${row.original.target_id?.slice(0, 8)}`
        : '—',
    enableSorting: false,
  }),
  columnHelper.accessor('detail', {
    header: 'Detail',
    cell: (info) => (
      <span className="text-gray-500 truncate block max-w-xs">{info.getValue()}</span>
    ),
    enableSorting: false,
  }),
]

export default function AuditLog() {
  const [sorting, setSorting] = useState<SortingState>([])
  const [actorFilter, setActorFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')

  const { data: entries = [], isLoading, error } = useQuery({
    queryKey: ['audit', actorFilter, actionFilter],
    queryFn: () => {
      const params = new URLSearchParams({ limit: '200' })
      if (actorFilter) params.set('actor_id', actorFilter)
      if (actionFilter) params.set('action', actionFilter)
      return api.get<AuditEntry[]>(`/api/audit?${params}`)
    },
  })

  const table = useReactTable({
    data: entries,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: 50 } },
  })

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

      {error && <p className="text-red-500">{(error as Error).message}</p>}

      {isLoading ? (
        <p className="text-gray-500">Loading…</p>
      ) : (
        <>
          <table className="w-full text-xs border-collapse">
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
                    No entries.
                  </td>
                </tr>
              ) : (
                table.getRowModel().rows.map((row) => (
                  <tr key={row.id} className="border-b hover:bg-gray-50">
                    {row.getVisibleCells().map((cell) => (
                      <td key={cell.id} className="py-1.5 pr-4">
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))
              )}
            </tbody>
          </table>

          {/* Pagination */}
          <div className="flex items-center gap-3 text-sm">
            <button
              onClick={() => table.previousPage()}
              disabled={!table.getCanPreviousPage()}
              className="px-2 py-1 border rounded disabled:opacity-40"
            >
              ← Prev
            </button>
            <span>
              Page {table.getState().pagination.pageIndex + 1} of {table.getPageCount()}
            </span>
            <button
              onClick={() => table.nextPage()}
              disabled={!table.getCanNextPage()}
              className="px-2 py-1 border rounded disabled:opacity-40"
            >
              Next →
            </button>
          </div>
        </>
      )}
    </div>
  )
}
