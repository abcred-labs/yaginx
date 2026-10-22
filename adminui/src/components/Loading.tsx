import { Spinner } from "@/ui/icons";
import React from "react";

export default function Loading() {
  return (
    <div className="flex min-h-40 w-full items-center justify-center text-muted-foreground" role="status" aria-label="加载中">
      <Spinner className="size-8 animate-spin" aria-hidden="true" />
    </div>
  );
}
