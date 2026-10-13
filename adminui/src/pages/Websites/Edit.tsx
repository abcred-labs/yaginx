import React from 'react';
import { WebsiteWorkspace } from './WebsiteWorkspace';

export const Edit: React.FC<{ section?: 'overview' | 'hosts' | 'routes' | 'responses' | 'access' | 'history' }> = ({ section = 'overview' }) => <WebsiteWorkspace section={section} />;
