import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  flexRender,
  createColumnHelper,
  type SortingState,
} from '@tanstack/react-table'
import { useEffect, useRef, useState } from 'react'
import { clsx } from 'clsx'
import { api, type Transfer } from '../api/client'

function ProgressBar({ done, total, complete }: { done: number; total?: number; complete?: boolean }) {
  const pct = complete ? 100 : (total && total > 0 ? Math.min(100, (done / total) * 100) : 0)
  return (
    <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
      <div
        className="bg-blue-500 h-full transition-all"
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}

const statusColors: Record<string, string> = {
  queued: 'bg-gray-100 text-gray-700',
  active: 'bg-blue-100 text-blue-800',
  paused: 'bg-yellow-100 text-yellow-800',
  complete: 'bg-green-100 text-green-800',
  failed: 'bg-red-100 text-red-800',
}

function formatBytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 ** 2) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`
  return `${(n / 1024 ** 3).toFixed(2)} GB`
}

const columnHelper = createColumnHelper<Transfer>()

function TransferActions({ transfer }: { transfer: Transfer }) {
  const qc = useQueryClient()

  const pause = useMutation({
    mutationFn: () => api.post(`/api/transfers/${transfer.id}/pause`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['transfers'] }),
  })
  const resume = useMutation({
    mutationFn: () => api.post(`/api/transfers/${transfer.id}/resume`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['transfers'] }),
  })
  const retry = useMutation({
    mutationFn: () => api.post(`/api/transfers/${transfer.id}/retry`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['transfers'] }),
  })

  return (
    <div className="flex flex-col gap-1">
      <div className="flex gap-2">
        {transfer.status === 'active' && (
          <button onClick={() => pause.mutate()} disabled={pause.isPending}
            className="text-yellow-600 hover:underline text-xs disabled:opacity-50">Pause</button>
        )}
        {transfer.status === 'paused' && (
          <button onClick={() => resume.mutate()} disabled={resume.isPending}
            className="text-blue-600 hover:underline text-xs disabled:opacity-50">Resume</button>
        )}
        {transfer.status === 'failed' && (
          <button onClick={() => retry.mutate()} disabled={retry.isPending}
            className="text-green-600 hover:underline text-xs disabled:opacity-50">Retry</button>
        )}
      </div>
      {transfer.status === 'failed' && transfer.error && (
        <p className="text-xs text-red-500 max-w-xs truncate" title={transfer.error}>{transfer.error}</p>
      )}
    </div>
  )
}

const columns = [
  columnHelper.display({
    id: 'item',
    header: 'Item',
    cell: ({ row }) => (
      <span className="text-sm font-medium">
        {row.original.item_title || <span className="font-mono text-xs text-gray-400">{row.original.id.slice(0, 8)}…</span>}
      </span>
    ),
  }),
  columnHelper.accessor('status', {
    header: 'Status',
    cell: (info) => (
      <span
        className={clsx(
          'px-2 py-0.5 rounded-full text-xs font-medium',
          statusColors[info.getValue()] ?? 'bg-gray-100',
        )}
      >
        {info.getValue()}
      </span>
    ),
  }),
  columnHelper.display({
    id: 'progress',
    header: 'Progress',
    cell: ({ row }) => (
      <div className="space-y-1 min-w-[160px]">
        <ProgressBar done={row.original.bytes_done} total={row.original.bytes_total} complete={row.original.status === 'complete'} />
        <span className="text-xs text-gray-500">
          {formatBytes(row.original.bytes_done)}
          {row.original.bytes_total ? ` / ${formatBytes(row.original.bytes_total)}` : ''}
        </span>
      </div>
    ),
    enableSorting: false,
  }),
  columnHelper.accessor('queued_at', {
    header: 'Queued',
    cell: (info) => new Date(info.getValue()).toLocaleString(),
  }),
  columnHelper.display({
    id: 'actions',
    header: 'Actions',
    cell: ({ row }) => <TransferActions transfer={row.original} />,
    enableSorting: false,
  }),
]

// useTransferWS subscribes to /api/ws and applies live progress events to the
// react-query cache so the table updates in real time without polling.
function useTransferWS() {
  const qc = useQueryClient()
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    const token = localStorage.getItem('session_token')
    if (!token) return

    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${window.location.host}/api/ws`)
    wsRef.current = ws

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data as string) as {
          type: string
          transfer_id: string
          bytes_done?: number
          bytes_total?: number
          status?: string
        }

        qc.setQueryData<Transfer[]>(['transfers'], (prev) => {
          if (!prev) return prev
          return prev.map((t) => {
            if (t.id !== msg.transfer_id) return t
            if (msg.type === 'transfer.progress') {
              return {
                ...t,
                bytes_done: msg.bytes_done ?? t.bytes_done,
                bytes_total: msg.bytes_total ?? t.bytes_total,
              }
            }
            if (msg.type === 'transfer.status' && msg.status) {
              return { ...t, status: msg.status as Transfer['status'] }
            }
            return t
          })
        })
      } catch {
        // Ignore malformed messages.
      }
    }

    ws.onerror = () => ws.close()

    return () => {
      ws.close()
      wsRef.current = null
    }
  }, [qc])
}

export default function Transfers() {
  const [sorting, setSorting] = useState<SortingState>([])

  const { data: transfers = [], isLoading, error } = useQuery({
    queryKey: ['transfers'],
    queryFn: () => api.get<Transfer[]>('/api/transfers'),
    refetchInterval: false, // Real-time updates come from the WebSocket.
  })

  useTransferWS()

  const table = useReactTable({
    data: transfers,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Transfers</h1>
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
                  No transfers.
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
