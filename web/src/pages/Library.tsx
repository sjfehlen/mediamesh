import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, Film, Book, Headphones, Tv } from 'lucide-react'
import { api, getTVSeries, type LibraryItem, type TVSeriesSummary } from '../api/client'
import ItemDetailPanel from '../components/ItemDetailPanel'

const mediaTypeIcons: Record<string, React.ReactNode> = {
  movie: <Film className="w-4 h-4" />,
  tvshow: <Tv className="w-4 h-4" />,
  audiobook: <Headphones className="w-4 h-4" />,
  ebook: <Book className="w-4 h-4" />,
}

function MediaCard({ item, onClick }: { item: LibraryItem; onClick: () => void }) {
  return (
    <div
      className="bg-white rounded-lg shadow overflow-hidden flex flex-col cursor-pointer hover:shadow-md hover:ring-2 hover:ring-blue-200 transition-shadow"
      onClick={onClick}
    >
      {item.poster_url ? (
        <img src={item.poster_url} alt={item.meta_title ?? item.title} className="w-full h-48 object-cover" />
      ) : (
        <div className="w-full h-48 bg-gray-200 flex items-center justify-center">
          <span className="text-gray-400 text-4xl">?</span>
        </div>
      )}
      <div className="p-3 flex-1 flex flex-col gap-1">
        <div className="flex items-center gap-1 text-xs text-gray-500">
          {mediaTypeIcons[item.media_type] ?? null}
          <span className="capitalize">{item.media_type}</span>
          {item.year && <span>· {item.year}</span>}
          {item.peer_id && <span className="ml-auto text-blue-500">Remote</span>}
        </div>
        <p className="font-medium text-sm leading-tight line-clamp-2">{item.meta_title ?? item.title}</p>
        {item.rating !== undefined && (
          <p className="text-xs text-gray-500">⭐ {item.rating.toFixed(1)}</p>
        )}
      </div>
    </div>
  )
}

function TVSeriesCard({ series }: { series: TVSeriesSummary }) {
  return (
    <div className="bg-white rounded-lg shadow overflow-hidden flex flex-col">
      {series.poster_url ? (
        <img src={series.poster_url} alt={series.series} className="w-full h-48 object-cover" />
      ) : (
        <div className="w-full h-48 bg-gray-200 flex items-center justify-center">
          <Tv className="w-10 h-10 text-gray-400" />
        </div>
      )}
      <div className="p-3 flex-1 flex flex-col gap-1">
        <div className="flex items-center gap-1 text-xs text-gray-500">
          <Tv className="w-4 h-4" />
          <span>{series.season_count} season{series.season_count !== 1 ? 's' : ''}</span>
          <span>· {series.episode_count} ep{series.episode_count !== 1 ? 's' : ''}</span>
        </div>
        <p className="font-medium text-sm leading-tight line-clamp-2">{series.series}</p>
        {series.rating != null && (
          <p className="text-xs text-gray-500">⭐ {series.rating.toFixed(1)}</p>
        )}
      </div>
    </div>
  )
}

export default function Library() {
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [selectedItemId, setSelectedItemId] = useState<string | null>(null)

  const isTVView = typeFilter === 'tvshow'

  const { data: items = [], isLoading: itemsLoading, error: itemsError } = useQuery({
    queryKey: ['library', search, typeFilter],
    queryFn: () => {
      const params = new URLSearchParams()
      if (search) params.set('search', search)
      if (typeFilter) params.set('type', typeFilter)
      return api.get<LibraryItem[]>(`/api/library?${params}`)
    },
    enabled: !isTVView,
  })

  const { data: tvSeries = [], isLoading: tvLoading, error: tvError } = useQuery({
    queryKey: ['tv-series'],
    queryFn: getTVSeries,
    enabled: isTVView,
  })

  const isLoading = isTVView ? tvLoading : itemsLoading
  const error = isTVView ? tvError : itemsError

  const filteredTV = isTVView && search
    ? tvSeries.filter((s) => s.series.toLowerCase().includes(search.toLowerCase()))
    : tvSeries

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Library</h1>

      <div className="flex gap-3 flex-wrap">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search titles…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9 pr-3 py-2 border rounded-md text-sm w-64"
          />
        </div>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="border rounded-md px-3 py-2 text-sm"
        >
          <option value="">All types</option>
          <option value="movie">Movies</option>
          <option value="tvshow">TV Shows</option>
          <option value="audiobook">Audiobooks</option>
          <option value="ebook">Ebooks</option>
        </select>
      </div>

      {error && <p className="text-red-500">{(error as Error).message}</p>}

      {isLoading ? (
        <p className="text-gray-500">Loading…</p>
      ) : isTVView ? (
        filteredTV.length === 0 ? (
          <p className="text-gray-500">No TV shows found.</p>
        ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
            {filteredTV.map((s) => (
              <TVSeriesCard key={s.series} series={s} />
            ))}
          </div>
        )
      ) : items.length === 0 ? (
        <p className="text-gray-500">No items found.</p>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
          {items.map((item) => (
            <MediaCard key={item.id} item={item} onClick={() => setSelectedItemId(item.id)} />
          ))}
        </div>
      )}

      <ItemDetailPanel itemId={selectedItemId} onClose={() => setSelectedItemId(null)} />
    </div>
  )
}
