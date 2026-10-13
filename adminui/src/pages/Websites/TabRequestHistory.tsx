import React from 'react';
import { Col, Form, InputNumber, Row, Select, Typography } from 'antd';

const { Text } = Typography;

export const TabRequestHistory = {
  label: '请求跟踪',
  key: 'tabRequestHistory',
  forceRender: true,
  children: <>
    <Text type="secondary">
      继承使用系统默认策略；仅错误保留错误请求；全量保留已完成请求，包含普通 404 和客户端取消的可观测结果。全局关闭时，本站点仍可选择仅错误或全量。
    </Text>
    <Row gutter={[8, 8]} style={{ marginTop: 'var(--space-4)' }}>
      <Col xs={24} md={12}>
        <Form.Item name={['requestHistory', 'mode']} label="跟踪模式" initialValue="inherit">
          <Select options={[
            { value: 'inherit', label: '继承系统设置' },
            { value: 'errorsOnly', label: '仅错误' },
            { value: 'all', label: '全量请求' },
          ]} />
        </Form.Item>
      </Col>
      <Col xs={24} md={12}>
        <Form.Item noStyle shouldUpdate={(previous, current) => previous.requestHistory?.mode !== current.requestHistory?.mode}>
          {({ getFieldValue }) => getFieldValue(['requestHistory', 'mode']) === 'all' ? (
            <Form.Item
              name={['requestHistory', 'successSamplingRate']}
              label="成功请求采样率"
              rules={[{ type: 'number', min: 0, max: 1, message: '请输入 0 到 1 之间的数值' }]}
              extra="留空表示 100%；错误请求始终完整记录。"
            >
              <InputNumber min={0} max={1} step={0.05} style={{ width: '100%' }} placeholder="1" />
            </Form.Item>
          ) : null}
        </Form.Item>
      </Col>
    </Row>
    <Text type="secondary">成功请求仅保存元数据和 Header。请求可能因策略过滤、成功采样、全局排除路径、队列丢弃、数据库写入失败或保留期清理而无法查询。站点策略保存后自动同步，历史数据不回填。</Text>
  </>
};
