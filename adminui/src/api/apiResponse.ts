import type { IApiRspEnvelop } from "./apiTypes";

export const validateResponse = <T>(response: IApiRspEnvelop<T>, validator: (value: T) => T): IApiRspEnvelop<T> => {
  if (response.code === 200) response.data = validator(response.data);
  return response;
};

export const validateRecordList = <P extends { records: unknown[] }>(response: IApiRspEnvelop<P>, validator: (value: P["records"][number]) => P["records"][number]): IApiRspEnvelop<P> =>
  validateResponse(response, (value) => ({ ...value, records: value.records.map(validator) }));

export const validateInt64Record = <T extends { id: string }>(record: T, validate: (value: string, label: string) => void, label: string): T => {
  validate(record.id, label);
  return record;
};
