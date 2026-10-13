import { SPACE } from "@/utils/spacing";
import { InfoCircleOutlined } from "@ant-design/icons";
import { Card, Col, Row, Statistic, Tooltip } from "antd";
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
  <Row gutter={[SPACE.space3, SPACE.space3]}>
    {items.map((item) => (
      <Col key={item.title} xs={24} sm={12} xl={6}>
        <Card loading={loading} className={`host-traffic-kpi host-traffic-kpi--${item.tone ?? "flow"}`}>
          <Statistic
            title={item.detail ? <Tooltip title={item.detail}><span className="host-traffic-kpi__title">{item.title}<InfoCircleOutlined aria-label={item.detail} /></span></Tooltip> : item.title}
            value={item.value}
          />
        </Card>
      </Col>
    ))}
  </Row>
);

export const useHostTrafficFilters = () => {
  const { filters, setFilters } = useHostTrafficWorkspace();
  return { filters, setFilters };
};

export const useHostTrafficRefresh = (enabled: boolean, seconds: number) => {
  const { refreshToken, refresh } = useHostTrafficWorkspace();
  return { refreshToken, refresh };
};
