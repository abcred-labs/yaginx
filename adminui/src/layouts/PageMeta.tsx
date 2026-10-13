import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

export interface IPageMeta {
  /** 面包屑尾段之前的上下文，形如 "GATEWAY / CANONICAL TRAFFIC" */
  trail?: string;
  title?: React.ReactNode;
  backTo?: string;
}

interface IPageMetaContext {
  meta: IPageMeta;
  setMeta: (meta: IPageMeta) => void;
}

const PageMetaContext = createContext<IPageMetaContext>({ meta: {}, setMeta: () => { } });

export const PageMetaProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  const [meta, setMeta] = useState<IPageMeta>({});
  const value = useMemo(() => ({ meta, setMeta }), [meta]);
  return <PageMetaContext.Provider value={value}>{children}</PageMetaContext.Provider>;
};

export const usePageMeta = () => useContext(PageMetaContext).meta;

/** 页面组件调用，向布局顶栏发布当前页面的面包屑信息。 */
export const usePublishPageMeta = (meta: IPageMeta) => {
  const { setMeta } = useContext(PageMetaContext);
  const { trail, title, backTo } = meta;
  const titleKey = typeof title === 'string' || typeof title === 'number' ? String(title) : '';
  useEffect(() => {
    setMeta({ trail, title, backTo });
    // title 为 ReactNode 时无法参与依赖比较，仅在可序列化时跟随变化
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trail, titleKey, backTo]);
};

/** 将 "GATEWAY / CANONICAL TRAFFIC" 拆成面包屑片段。 */
export const splitTrail = (trail?: string): string[] =>
  (trail ?? '')
    .split('/')
    .map((segment) => segment.trim())
    .filter(Boolean);
