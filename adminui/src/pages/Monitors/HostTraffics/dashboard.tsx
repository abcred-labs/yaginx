import { Card, CardContent, Statistic, Tooltip, TooltipContent, TooltipTrigger } from "@modra/react";
import { Info } from "lucide-react";
import { StatDot } from "@/ui/monParts";
import React, { createContext, useContext, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { HostTrafficFilters, filtersFromSearchParams, filtersToSearchParams } from "./shared";

export interface HostTrafficKpiItem {
  title: string;
  value: string;
  detail?: string;
  tone?: "flow" | "signal" | "warning" | "alert";
}

interface HostTrafficWorkspaceValue {
  filters: HostTrafficFilters;
  setFilters: (filters: HostTrafficFilters) => void;
  refreshToken: number;
  refresh: () => void;
  autoRefreshSeconds: number;
  setAutoRefreshSeconds: (seconds: number) => void;
  autoRefreshEnabled: boolean;
  setAutoRefreshEnabled: (enabled: boolean) => void;
}

const HostTrafficWorkspaceContext = createContext<HostTrafficWorkspaceValue | null>(null);

export const HostTrafficWorkspace: React.FC<React.PropsWithChildren> = ({ children }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const filters = useMemo(() => filtersFromSearchParams(searchParams), [searchParams]);
  const [refreshToken, setRefreshToken] = useState(0);
  const [autoRefreshSeconds, setAutoRefreshSeconds] = useState(0);
  const [autoRefreshEnabled, setAutoRefreshEnabled] = useState(false);

  useEffect(() => {
    if (searchParams.get("from") && searchParams.get("to")) return;
    setSearchParams(filtersToSearchParams(filters), { replace: true });
  }, [filters, searchParams, setSearchParams]);

  useEffect(() => {
    if (!autoRefreshEnabled || autoRefreshSeconds <= 0) return;
    const timer = window.setInterval(() => setRefreshToken((current) => current + 1), autoRefreshSeconds * 1000);
    return () => window.clearInterval(timer);
  }, [autoRefreshEnabled, autoRefreshSeconds]);

  const value = useMemo<HostTrafficWorkspaceValue>(() => ({
    filters,
    setFilters: (next) => setSearchParams(filtersToSearchParams(next)),
    refreshToken,
    refresh: () => setRefreshToken((current) => current + 1),
    autoRefreshSeconds,
    setAutoRefreshSeconds,
    autoRefreshEnabled,
    setAutoRefreshEnabled,
  }), [autoRefreshEnabled, autoRefreshSeconds, filters, refreshToken, setSearchParams]);

  return <HostTrafficWorkspaceContext.Provider value={value}>{children}</HostTrafficWorkspaceContext.Provider>;
};

export const useHostTrafficWorkspace = (): HostTrafficWorkspaceValue => {
  const value = useContext(HostTrafficWorkspaceContext);
  if (!value) throw new Error("useHostTrafficWorkspace must be used inside HostTrafficWorkspace");
  return value;
};

export const HostTrafficKpiCards: React.FC<{ items: HostTrafficKpiItem[]; loading?: boolean }> = ({ items, loading = false }) => (
  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
    {items.map((item) => {
      const title = (
        <span className="flex items-center gap-1.5 whitespace-nowrap">
          <StatDot tone={item.tone ?? "flow"} />
          {item.detail ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-flex cursor-help items-center gap-[5px]">{item.title}<Info className="size-[11px] text-muted-foreground" aria-label={item.detail} /></span>
              </TooltipTrigger>
              <TooltipContent>{item.detail}</TooltipContent>
            </Tooltip>
          ) : item.title}
        </span>
      );
      return (
        <Card key={item.title} className="h-full">
          <CardContent className="px-[14px] py-[10px] pt-[10px]">
            <Statistic
              className="flex items-center justify-between gap-2 space-y-0 [&>div:last-child]:min-w-0 [&>div:last-child]:break-all [&>div:last-child]:text-right [&>div:last-child]:text-[21px] [&>div:last-child]:leading-tight"
              title={title}
              value={item.value}
              loading={loading}
            />
          </CardContent>
        </Card>
      );
    })}
  </div>
);

export const useHostTrafficFilters = () => {
  const { filters, setFilters } = useHostTrafficWorkspace();
  return { filters, setFilters };
};

export const useHostTrafficRefresh = (enabled: boolean, seconds: number) => {
  const { refreshToken, refresh } = useHostTrafficWorkspace();
  return { refreshToken, refresh };
};
