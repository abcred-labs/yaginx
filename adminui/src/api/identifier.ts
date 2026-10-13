export type Int64 = string;

const decimalInt64 = /^-?[0-9]+$/;

export class IdentifierContractError extends Error {
  constructor(field = "ID") {
    super(`接口 ID 格式不兼容：${field} 必须是字符串。`);
    this.name = "IdentifierContractError";
  }
}

export const requireInt64String = (value: unknown, field = "ID"): Int64 => {
  if (typeof value !== "string" || !decimalInt64.test(value)) {
    throw new IdentifierContractError(field);
  }
  return value;
};
