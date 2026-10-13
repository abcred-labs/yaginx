// Compatibility export for existing pages. New code should import the
// explicit server-paged component so its request contract is obvious.
export { default } from "./ServerPagedTable";
export type { ServerPagedTableProps as TablePagedListProps } from "./ServerPagedTable";
