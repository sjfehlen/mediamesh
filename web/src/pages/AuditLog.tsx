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

const ERROR_CODE_LABELS: Record<string, string> = {
  PEER_UNREACHABLE:   'Peer unreachable',
  PEER_AUTH_FAILED:   'Peer auth failed',
  SYNC_PUSH_FAILED:   'Sync push failed',
  SYNC_PULL_FAILED:   'Sync pull failed',
  TRANSFER_FAILED:    'Transfer failed',
  TRANSFER_DUPLICATE: 'Duplicate detected',
  TRANSFER_DISK_FULL: 'Disk full',
  SCAN_FAILED:        'Scan failed',
  METADATA_FAILED:    'Metadata failed',
  HANDSHAKE_FAILED:   'Handshake failed',
  AUTH_FAILED:        'Auth failed',
}

function ErrorBadge({ code }: { code: string }) {
  return (
    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium bg-red-100 text-red-700 whitespace-nowrap">
      {code}
      {ERROR_CODE_LABELS[code] && (
        <span className="text-red-500 font-normal">· {ERROR_CODE_LABELS[code]}</span>
      )}
    </span>
  )
}

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
      <span className="font-mono text-xs">
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
    id: 'error_code',
    header: 'Error',
    cell: ({ row }) =>
      row.original.error_code ? <ErrorBadge code={row.original.error_code} /> : null,
    enableSorting: false,
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
      <span className="text-gray-500 truncate block max-w-xs text-xs">{info.getValue()}</span>
    ),
    enableSorting: false,
  }),
]

export default function AuditLog() {
  const [sorting, setSorting] = useState<SortingState>([])
  const [actorFilter, setActorFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [errorsOnly, setErrorsOnly] = useState(false)

  const { data: entries = [], isLoading, error } = useQuery({
    queryKey: ['audit', actorFilter, actionFilter, errorsOnly],
    queryFn: () => {
      const params = new URLSearchParams({ limit: '200' })
      if (actorFilter) params.set('actor_id', actorFilter)
      if (actionFilter) params.set('action', actionFilter)
      if (errorsOnly) params.set('errors_only', 'true')
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

  const errorCount = entries.filter((e) => e.error_code).length

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Audit Log</h1>
        {errorCount > 0 && (
          <span className="text-sm text-red-600 font-medium">
            {errorCount} error{errorCount !== 1 ? 's' : ''} in current view
          </span>
        )}
      </div>

      <div className="flex gap-3 flex-wrap items-center">
        <input
          type="text"
          placeholder="Filter by actor ID…"
          value={actorFilter}
          onChange={(e) => setActorFilter(e.target.value)}
          className="border rounded-md px-3 py-2 text-sm w-48"
        />
        <input
          type="text"
          placeholder="Filter by action…"
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          className="border rounded-md px-3 py-2 text-sm w-48"
        />
        <label className="flex items-center gap-2 text-sm cursor-pointer select-none">
          <input
            type="checkbox"
            checked={errorsOnly}
            onChange={(e) => setErrorsOnly(e.target.checked)}
            className="rounded"
          />
          <span className="text-red-600 font-medium">Errors only</span>
        </label>
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
                      className="pb-2 pr-4 select-none cursor-pointer font-semibold text-gray-700"
                      onClick={header.column.getToggleSortingHandler()}
                    >
                      {flexRender(header.column.columnDef.header, header.getContext())}
                      {header.column.getIsSorted() === 'asc' ? ' ↑'
                        : header.column.getIsSorted() === 'desc' ? ' ↓' : ''}
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
                  <tr
                    key={row.id}
                    className={row.original.error_code
                      ? 'border-b bg-red-50 hover:bg-red-100'
                      : 'border-b hover:bg-gray-50'}
                  >
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
