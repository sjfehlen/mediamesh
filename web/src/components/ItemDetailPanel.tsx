import { X, HardDrive, Film, Book, Headphones, Tv, Pencil, Check, XCircle } from 'lucide-react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getItem, getPeers, submitRequest, patchItem } from '../api/client'
import { useState } from 'react'

const mediaTypeIcons: Record<string, React.ReactNode> = {
  movie: <Film className="w-5 h-5" />,
  tvshow: <Tv className="w-5 h-5" />,
  tvepisode: <Tv className="w-5 h-5" />,
  tvseason: <Tv className="w-5 h-5" />,
  audiobook: <Headphones className="w-5 h-5" />,
  ebook: <Book className="w-5 h-5" />,
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

interface Props {
  itemId: string | null
  onClose: () => void
}

export default function ItemDetailPanel({ itemId, onClose }: Props) {
  const queryClient = useQueryClient()
  const [requestNote, setRequestNote] = useState('')
  const [requestSent, setRequestSent] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editTitle, setEditTitle] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [editPosterUrl, setEditPosterUrl] = useState('')
  const [editRating, setEditRating] = useState('')

  const { data: item, isLoading: itemLoading } = useQuery({
    queryKey: ['library-item', itemId],
    queryFn: () => getItem(itemId!),
    enabled: itemId !== null,
  })

  const { data: peers = [] } = useQuery({
    queryKey: ['peers'],
    queryFn: getPeers,
    enabled: itemId !== null,
  })

  const requestMutation = useMutation({
    mutationFn: () => submitRequest(itemId!, requestNote || undefined),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['requests'] })
      setRequestSent(true)
    },
  })

  const editMutation = useMutation({
    mutationFn: () => patchItem(itemId!, {
      meta_title: editTitle || undefined,
      description: editDescription || undefined,
      poster_url: editPosterUrl || undefined,
      rating: editRating ? parseFloat(editRating) : undefined,
    }),
    onSuccess: (updated) => {
      queryClient.setQueryData(['library-item', itemId], updated)
      queryClient.invalidateQueries({ queryKey: ['library'] })
      setEditing(false)
    },
  })

  function startEditing() {
    if (!item) return
    setEditTitle(item.meta_title ?? item.title)
    setEditDescription(item.description ?? '')
    setEditPosterUrl(item.poster_url ?? '')
    setEditRating(item.rating?.toString() ?? '')
    setEditing(true)
  }

  const isRemote = item?.peer_id != null
  const hostName = isRemote
    ? peers.find((p) => p.id === item?.peer_id)?.display_name ?? 'Unknown peer'
    : 'Local'

  const displayTitle = item?.meta_title ?? item?.title ?? 'Loading…'

  if (!itemId) return null

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 bg-black/30 z-40" onClick={onClose} />

      {/* Panel */}
      <div className="fixed right-0 top-0 h-full w-full max-w-md bg-white shadow-xl z-50 flex flex-col overflow-y-auto">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b sticky top-0 bg-white">
          <h2 className="font-semibold text-lg truncate pr-4">{displayTitle}</h2>
          <div className="flex items-center gap-2 shrink-0">
            {item && !isRemote && !editing && (
              <button onClick={startEditing} className="text-gray-400 hover:text-gray-700" title="Edit metadata">
                <Pencil className="w-4 h-4" />
              </button>
            )}
            <button onClick={onClose} className="text-gray-400 hover:text-gray-700">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {itemLoading && <p className="p-6 text-gray-500">Loading…</p>}

        {item && !editing && (
          <div className="flex flex-col gap-5 p-5">
            {/* Poster */}
            {item.poster_url ? (
              <img src={item.poster_url} alt={displayTitle} className="w-full rounded-lg object-cover max-h-72" />
            ) : (
              <div className="w-full h-48 bg-gray-100 rounded-lg flex items-center justify-center text-gray-300">
                {mediaTypeIcons[item.media_type] ?? <Film className="w-10 h-10" />}
              </div>
            )}

            {/* Meta row */}
            <div className="flex flex-wrap gap-2 text-sm text-gray-500">
              <span className="flex items-center gap-1">
                {mediaTypeIcons[item.media_type]}
                <span className="capitalize">{item.media_type}</span>
              </span>
              {item.year && <span>· {item.year}</span>}
              {item.rating != null && <span>· ⭐ {item.rating.toFixed(1)}</span>}
              {item.series && (
                <span>· {item.series}{item.season_num != null ? ` S${item.season_num}` : ''}</span>
              )}
            </div>

            {item.meta_title && item.meta_title !== item.title && (
              <p className="text-xs text-gray-400">File: {item.title}</p>
            )}

            {item.description && (
              <p className="text-sm text-gray-700 leading-relaxed">{item.description}</p>
            )}

            {/* File info */}
            <div className="bg-gray-50 rounded-lg p-3 flex flex-col gap-2 text-sm">
              <div className="flex items-center gap-2 text-gray-600">
                <HardDrive className="w-4 h-4 shrink-0" />
                <span>Hosted on: <span className="font-medium text-gray-900">{hostName}</span></span>
              </div>
              {item.file_size != null && (
                <div className="text-gray-500 pl-6">Size: {formatBytes(item.file_size)}</div>
              )}
            </div>

            {/* Request transfer — remote items only */}
            {isRemote && (
              <div className="flex flex-col gap-2">
                {requestSent ? (
                  <p className="text-green-600 text-sm font-medium">Request submitted!</p>
                ) : (
                  <>
                    <textarea
                      placeholder="Optional note…"
                      value={requestNote}
                      onChange={(e) => setRequestNote(e.target.value)}
                      className="border rounded-md px-3 py-2 text-sm resize-none h-20"
                    />
                    <button
                      onClick={() => requestMutation.mutate()}
                      disabled={requestMutation.isPending}
                      className="bg-blue-600 text-white rounded-md px-4 py-2 text-sm font-medium hover:bg-blue-700 disabled:opacity-50"
                    >
                      {requestMutation.isPending ? 'Requesting…' : 'Request transfer'}
                    </button>
                    {requestMutation.isError && (
                      <p className="text-red-500 text-sm">{(requestMutation.error as Error).message}</p>
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        )}

        {/* Edit form */}
        {item && editing && (
          <div className="flex flex-col gap-4 p-5">
            <p className="text-xs text-gray-500">File name: {item.title}</p>

            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">Title</label>
              <input
                type="text"
                value={editTitle}
                onChange={(e) => setEditTitle(e.target.value)}
                className="w-full border rounded-md px-3 py-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">Description</label>
              <textarea
                value={editDescription}
                onChange={(e) => setEditDescription(e.target.value)}
                rows={5}
                className="w-full border rounded-md px-3 py-2 text-sm resize-none"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">Poster URL</label>
              <input
                type="text"
                value={editPosterUrl}
                onChange={(e) => setEditPosterUrl(e.target.value)}
                className="w-full border rounded-md px-3 py-2 text-sm font-mono"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">Rating (0–10)</label>
              <input
                type="number"
                min="0"
                max="10"
                step="0.1"
                value={editRating}
                onChange={(e) => setEditRating(e.target.value)}
                className="w-32 border rounded-md px-3 py-2 text-sm"
              />
            </div>

            {editMutation.isError && (
              <p className="text-red-500 text-sm">{(editMutation.error as Error).message}</p>
            )}

            <div className="flex gap-2">
              <button
                onClick={() => editMutation.mutate()}
                disabled={editMutation.isPending}
                className="flex items-center gap-1 px-4 py-2 bg-blue-600 text-white rounded-md text-sm hover:bg-blue-700 disabled:opacity-50"
              >
                <Check className="w-4 h-4" />
                {editMutation.isPending ? 'Saving…' : 'Save'}
              </button>
              <button
                onClick={() => setEditing(false)}
                className="flex items-center gap-1 px-4 py-2 border rounded-md text-sm hover:bg-gray-50"
              >
                <XCircle className="w-4 h-4" />
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>
    </>
  )
}
