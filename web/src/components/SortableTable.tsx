import { useState } from 'react'
import { Table, type TableProps } from 'antd'
import type { ColumnType } from 'antd/es/table'
import type { SorterResult } from 'antd/es/table/interface'

// A column may say how to sort it when its value is not simply row[dataIndex]
// (a rendered name built from two fields, a count of a list), or opt out.
export type SortableColumn<T> = ColumnType<T> & {
  sortValue?: (row: T) => unknown
  sortable?: false
}

const natural = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })
const isoDate = /^\d{4}-\d{2}-\d{2}T/

// compareValues orders the values tables hold: numbers as numbers, ISO
// timestamps as times, arrays by length, and text naturally — "item2"
// before "item10", "2.9" before "2.10". Empty values go last whichever way
// the column is sorted, so a click never fills the first page with blanks.
export function compareValues(a: unknown, b: unknown): number {
  const empty = (v: unknown) => v === null || v === undefined || v === ''
  if (empty(a) || empty(b)) return empty(a) === empty(b) ? 0 : empty(a) ? 1 : -1
  if (typeof a === 'number' && typeof b === 'number') return a - b
  if (typeof a === 'boolean' && typeof b === 'boolean') return Number(a) - Number(b)
  if (Array.isArray(a) && Array.isArray(b)) return a.length - b.length
  if (typeof a === 'string' && typeof b === 'string') {
    if (isoDate.test(a) && isoDate.test(b)) return Date.parse(a) - Date.parse(b)
    return natural.compare(a, b)
  }
  return natural.compare(String(a), String(b))
}

function valueOf<T>(row: T, col: SortableColumn<T>): unknown {
  if (col.sortValue) return col.sortValue(row)
  const di = col.dataIndex
  if (di === undefined || di === null) return undefined
  const path = Array.isArray(di) ? di : [di]
  let v: unknown = row
  for (const k of path) v = v == null ? undefined : (v as Record<string | number, unknown>)[k as string]
  return v
}

// SortableTable is antd's Table with every data column sortable by clicking
// its header, for tables that hold all their rows. Tables paged by the
// server sort on the server instead (sorter: true), because sorting one
// page of fifty in the browser would put the wrong fifty rows on it.
export function SortableTable<T extends object>(props: Omit<TableProps<T>, 'columns'> & { columns?: SortableColumn<T>[] }) {
  const columns = props.columns?.map((col) => {
    if (col.sorter !== undefined || col.sortable === false) return col
    if (col.dataIndex === undefined && !col.sortValue) return col
    const empties = (row: T) => {
      const v = valueOf(row, col)
      return v === null || v === undefined || v === ''
    }
    return {
      ...col,
      showSorterTooltip: false,
      sorter: (a: T, b: T, order?: 'ascend' | 'descend' | null) => {
        // Keep blanks last in both directions: antd reverses the result for
        // descending, so reverse the blank rule to cancel it out.
        if (empties(a) !== empties(b)) return (empties(a) ? 1 : -1) * (order === 'descend' ? -1 : 1)
        return compareValues(valueOf(a, col), valueOf(b, col))
      },
    }
  })
  return <Table<T> {...props} columns={columns as TableProps<T>['columns']} />
}

type ServerSort = { key?: string; order?: 'ascend' | 'descend' }

// useServerSort drives header sorting for a table the server pages: the
// click becomes sort/order request parameters, and the page goes back to
// the first — staying on page 5 of a new order shows an arbitrary slice.
export function useServerSort(onReset?: () => void) {
  const [s, setS] = useState<ServerSort>({})
  const onChange = (_p: unknown, _f: unknown, sorter: SorterResult<any> | SorterResult<any>[]) => {
    const x = Array.isArray(sorter) ? sorter[0] : sorter
    const next: ServerSort = x?.order ? { key: String(x.columnKey ?? x.field), order: x.order } : {}
    if (next.key !== s.key || next.order !== s.order) {
      setS(next)
      onReset?.()
    }
  }
  // Spread into a column: col('name') makes it sortable by the server.
  const col = (key: string) => ({ key, sorter: true, showSorterTooltip: false, sortOrder: s.key === key ? s.order ?? null : null })
  const params: Record<string, string> = s.key ? { sort: s.key, order: s.order === 'descend' ? 'desc' : 'asc' } : {}
  return { onChange, col, params }
}
