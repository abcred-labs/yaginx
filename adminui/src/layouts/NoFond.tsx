import { Button, Result } from '@modra/react';
import React from 'react';
import { Link } from 'react-router-dom';

const NoFound: React.FC = () => (
  <Result
    status="warning"
    title="404"
    subTitle="页面不存在或已被移除。"
    extra={<Button asChild><Link to="/">返回首页</Link></Button>}
  />
);
export default NoFound;
