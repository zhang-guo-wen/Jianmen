import type { IssuedAIAccessToken } from '../api/client';

const DEFAULT_PROMPT = '\u4f60\u53ef\u4ee5\u4f7f\u7528\u6211\u7684\u6743\u9650\u8bbf\u95ee\u6211\u7684\u670d\u52a1\u5668\u3001\u6570\u636e\u5e93\u7b49\u8d44\u6e90\uff0c';

export function buildAIAuthorizationPrompt(result: IssuedAIAccessToken): string {
  const accessTokenLine = `\u8bbf\u95ee\u4ee4\u724c\uff1a${result.access_token}`;
  const refreshTokenLine = `\u5237\u65b0\u4ee4\u724c\uff1a${result.refresh_token}`;
  const docsURL = result.docs_url?.trim() || '';
  let prompt = (result.copy_prompt?.trim() || DEFAULT_PROMPT)
    .replace(/<access_token>/g, () => result.access_token)
    .replace(/<refresh_token>/g, () => result.refresh_token);

  if (result.access_token && !prompt.includes(result.access_token)) prompt += `\n${accessTokenLine}`;
  if (result.refresh_token && !prompt.includes(result.refresh_token)) prompt += `\n${refreshTokenLine}`;
  if (docsURL && !prompt.includes(docsURL)) prompt += `\n\u5177\u4f53\u89c1\u6587\u6863\uff1a${docsURL}`;

  return prompt;
}
